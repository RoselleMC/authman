package com.iroselle.authman.bootstrap

import com.google.gson.Gson
import com.iroselle.authman.bridge.protocol.BridgeMessage
import com.iroselle.authman.bridge.protocol.BridgeRequest
import com.iroselle.authman.bridge.protocol.BridgeResponse
import com.iroselle.authman.bridge.protocol.CHANNEL_NAME
import com.iroselle.authman.bridge.protocol.MAX_ARGUMENTS
import com.iroselle.authman.bridge.protocol.MAX_ARGUMENT_LENGTH
import com.iroselle.authman.bridge.protocol.MAX_PAYLOAD_BYTES
import com.iroselle.authman.bridge.protocol.PROTOCOL_VERSION
import com.iroselle.authman.bridge.protocol.validRequestId
import com.iroselle.authman.spi.AuthmanCommandActor
import com.iroselle.authman.spi.AuthmanCommandActorKind
import com.iroselle.authman.spi.AuthmanCommandMessage
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.PluginMessageEvent
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.proxy.ProxyServer
import com.velocitypowered.api.proxy.ServerConnection
import com.velocitypowered.api.proxy.messages.MinecraftChannelIdentifier
import org.slf4j.Logger
import java.nio.charset.StandardCharsets
import java.util.UUID

class BridgeChannel(
    private val server: ProxyServer,
    private val logger: Logger,
    private val pluginOwner: Any,
    private val router: RuntimeCommandRouter,
) : AutoCloseable {
    private val gson = Gson()
    private val channel = MinecraftChannelIdentifier.from(CHANNEL_NAME)
    private val routerListener: AutoCloseable

    init {
        server.channelRegistrar.register(channel)
        routerListener = router.addChangeListener(::broadcastState)
    }

    @Subscribe
    fun onPluginMessage(event: PluginMessageEvent) {
        if (event.identifier != channel) {
            return
        }
        event.setResult(PluginMessageEvent.ForwardResult.handled())
        val backend = event.source as? ServerConnection ?: return
        val payload = event.data
        if (payload.isEmpty() || payload.size > MAX_PAYLOAD_BYTES) {
            logger.warn("Rejected Authman bridge payload from {}: invalid size {}", backend.serverInfo.name, payload.size)
            return
        }
        val request = try {
            gson.fromJson(String(payload, StandardCharsets.UTF_8), BridgeRequest::class.java)
        } catch (ex: Exception) {
            logger.warn("Rejected malformed Authman bridge payload from {}", backend.serverInfo.name, ex)
            return
        }
        server.scheduler.buildTask(pluginOwner, Runnable { process(backend, request) }).schedule()
    }

    override fun close() {
        runCatching { routerListener.close() }
        server.channelRegistrar.unregister(channel)
    }

    private fun process(backend: ServerConnection, request: BridgeRequest) {
        if (request.version != PROTOCOL_VERSION) {
            respondError(backend, request, "unsupported bridge protocol ${request.version}")
            return
        }
        if (!validRequestId(request.requestId)) {
            respondError(backend, request, "invalid request id")
            return
        }
        if (request.arguments.size > MAX_ARGUMENTS || request.arguments.any { it.length > MAX_ARGUMENT_LENGTH }) {
            respondError(backend, request, "command arguments exceed the bridge limit")
            return
        }
        val actor = createActor(backend, request) ?: run {
            respondError(backend, request, "bridge sender identity does not match its Velocity connection")
            return
        }
        val response = when (request.type.lowercase()) {
            "execute" -> {
                val handled = router.execute(actor, request.arguments)
                BridgeResponse(
                    type = "result",
                    requestId = request.requestId,
                    actorId = actor.uniqueId.orEmpty(),
                    success = handled,
                    available = router.isOperational(),
                    messages = actor.messages(),
                )
            }
            "suggest" -> BridgeResponse(
                type = "suggestions",
                requestId = request.requestId,
                actorId = actor.uniqueId.orEmpty(),
                success = true,
                available = router.isOperational(),
                suggestions = router.suggest(actor, request.arguments),
            )
            "capabilities" -> BridgeResponse(
                type = "capabilities",
                requestId = request.requestId,
                actorId = actor.uniqueId.orEmpty(),
                success = true,
                available = router.isOperational(),
                suggestions = router.suggest(actor, listOf("")),
            )
            else -> {
                respondError(backend, request, "unknown bridge request type")
                return
            }
        }
        send(backend, response)
    }

    private fun createActor(backend: ServerConnection, request: BridgeRequest): CollectingActor? {
        return when (request.sender.kind.lowercase()) {
            "player" -> {
                val requested = runCatching { UUID.fromString(request.sender.uniqueId) }.getOrNull() ?: return null
                val player = backend.player
                if (requested != player.uniqueId) {
                    return null
                }
                CollectingActor(player, AuthmanCommandActorKind.PLAYER, player.username, player.uniqueId.toString())
            }
            "console" -> CollectingActor(
                player = null,
                kind = AuthmanCommandActorKind.BRIDGE_CONSOLE,
                name = "${backend.serverInfo.name}:console",
                uniqueId = null,
            )
            else -> null
        }
    }

    private fun respondError(backend: ServerConnection, request: BridgeRequest, error: String) {
        send(
            backend,
            BridgeResponse(
                type = "error",
                requestId = request.requestId.takeIf(::validRequestId).orEmpty(),
                actorId = request.sender.uniqueId,
                success = false,
                available = router.isOperational(),
                messages = listOf(BridgeMessage(text = error, tone = "error")),
                error = error,
            ),
        )
    }

    private fun send(backend: ServerConnection, response: BridgeResponse) {
        val payload = gson.toJson(response).toByteArray(StandardCharsets.UTF_8)
        if (payload.size > MAX_PAYLOAD_BYTES) {
            logger.warn("Authman bridge response exceeded the payload limit request={}", response.requestId)
            return
        }
        if (!backend.sendPluginMessage(channel, payload)) {
            logger.debug("Authman bridge response could not be delivered request={} server={}", response.requestId, backend.serverInfo.name)
        }
    }

    private fun broadcastState() {
        val payload = gson.toJson(
            BridgeResponse(
                type = "state",
                success = true,
                available = router.isOperational(),
                suggestions = listOf("reload"),
            ),
        ).toByteArray(StandardCharsets.UTF_8)
        server.scheduler.buildTask(pluginOwner, Runnable {
            server.allServers.forEach { registered ->
                runCatching { registered.sendPluginMessage(channel, payload) }
            }
        }).schedule()
    }

    private class CollectingActor(
        private val player: Player?,
        override val kind: AuthmanCommandActorKind,
        override val name: String,
        override val uniqueId: String?,
    ) : AuthmanCommandActor {
        private val output = mutableListOf<AuthmanCommandMessage>()

        override fun hasPermission(permission: String): Boolean = player?.hasPermission(permission) ?: true

        override fun send(message: AuthmanCommandMessage) {
            if (output.size < MAX_MESSAGES) {
                output += message.copy(text = message.text.take(MAX_MESSAGE_LENGTH))
            }
        }

        fun messages(): List<BridgeMessage> = output.map { message ->
            BridgeMessage(
                text = message.text,
                tone = message.tone.name.lowercase(),
                actionType = message.action?.type?.name?.lowercase().orEmpty(),
                actionValue = message.action?.value.orEmpty(),
            )
        }

        companion object {
            private const val MAX_MESSAGES = 32
            private const val MAX_MESSAGE_LENGTH = 2000
        }
    }
}
