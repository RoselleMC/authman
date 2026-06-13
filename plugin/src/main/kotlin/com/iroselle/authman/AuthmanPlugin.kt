package com.iroselle.authman

import com.google.inject.Inject
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent
import com.velocitypowered.api.event.proxy.ProxyShutdownEvent
import com.velocitypowered.api.plugin.Plugin
import com.velocitypowered.api.proxy.ProxyServer
import com.iroselle.authman.api.AuthmanClient
import com.iroselle.authman.command.AuthmanCommand
import com.iroselle.authman.config.AuthmanConfig
import com.iroselle.authman.config.InstanceFingerprint
import com.iroselle.authman.listener.DownstreamAuthListener
import com.iroselle.authman.message.AuthmanMessages
import com.iroselle.authman.model.DownstreamServerOption
import com.iroselle.authman.model.DownstreamStatusReport
import com.iroselle.authman.model.NodeAction
import com.iroselle.authman.model.NodeActionAck
import net.kyori.adventure.key.Key
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import org.slf4j.Logger
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.nio.file.Path
import java.util.concurrent.TimeUnit
import kotlin.math.min

@Plugin(
    id = "authman",
    name = "authman",
    version = AuthmanPlugin.VERSION,
    description = "Authman Velocity integration plugin",
    authors = ["Score2"],
)
class AuthmanPlugin @Inject constructor(
    private val server: ProxyServer,
    private val logger: Logger,
    @com.velocitypowered.api.plugin.annotation.DataDirectory private val dataDirectory: Path,
) {
    private lateinit var config: AuthmanConfig
    private lateinit var instanceFingerprint: String
    private lateinit var client: AuthmanClient
    private lateinit var messages: AuthmanMessages
    @Volatile
    private var coreAccessRevoked: Boolean = false
    @Volatile
    private var downstreamTransferTargets: List<DownstreamServerOption> = emptyList()
    @Volatile
    private var eventLoopRunning: Boolean = false
    @Volatile
    private var heartbeatLoopRunning: Boolean = false
    private lateinit var downstreamListener: DownstreamAuthListener

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = AuthmanConfig.load(dataDirectory)
        instanceFingerprint = InstanceFingerprint.load(dataDirectory)
        client = AuthmanClient(config, instanceFingerprint)
        messages = AuthmanMessages()

        downstreamListener = DownstreamAuthListener(this, server, logger, config, client, messages)
        server.eventManager.register(this, downstreamListener)
        val command = AuthmanCommand(this, logger)
        val meta = server.commandManager.metaBuilder("authman")
            .aliases("am")
            .plugin(this)
            .build()
        server.commandManager.register(meta, command)

        logger.info(
            "Authman downstream plugin enabled for {} with API {} instance={}",
            server.version.name,
            config.apiBase,
            instanceFingerprint,
        )
        sendHeartbeat()
        startHeartbeatLoop()
        startEventLoop()
    }

    @Subscribe
    fun onProxyShutdown(event: ProxyShutdownEvent) {
        eventLoopRunning = false
        heartbeatLoopRunning = false
    }

    fun reconnectNow(): Boolean = sendHeartbeat()

    fun reloadConfigAndReconnect(): Boolean {
        val next = AuthmanConfig.load(dataDirectory)
        config.replaceLocal(next)
        return sendHeartbeat()
    }

    fun client(): AuthmanClient = client

    private fun sendHeartbeat(): Boolean {
        try {
            val response = client.heartbeat(
                pluginVersion = VERSION,
                velocityVersion = server.version.version,
                status = currentStatusReport(),
            )
            if (!response.ok) {
                logger.warn("Authman heartbeat failed with HTTP {}: {}", response.statusCode, response.body)
                if (response.accessRevoked) {
                    lockFromCore("Core revoked or rejected this node token")
                }
                return false
            }
            applySync(response, "heartbeat")
            return true
        } catch (ex: Exception) {
            logger.warn("Authman heartbeat failed", ex)
            return false
        }
    }

    private fun currentStatusReport(): DownstreamStatusReport =
        DownstreamStatusReport(
            onlinePlayers = if (::downstreamListener.isInitialized) {
                downstreamListener.onlinePresenceCount()
            } else {
                server.allPlayers.count { it.isActive }
            },
            maxPlayers = server.configuration.showMaxPlayers,
        )

    private fun applySync(response: com.iroselle.authman.api.HeartbeatResult, source: String) {
        response.runtime?.let {
            config.applyRuntime(it)
        }
        downstreamTransferTargets = response.downstreamServers
        messages.apply(response.playerMessages)
        val acked = executeNodeActions(response.actions)
        if (acked.isNotEmpty()) {
            try {
                client.ackActions(acked)
            } catch (ex: Exception) {
                logger.warn("Failed to ACK Authman node actions {}", acked, ex)
            }
        }
        if (coreAccessRevoked) {
            logger.info("Authman Core access restored through {}", source)
        }
        coreAccessRevoked = false
    }

    private fun startHeartbeatLoop() {
        if (heartbeatLoopRunning) {
            return
        }
        heartbeatLoopRunning = true
        val thread = Thread {
            while (heartbeatLoopRunning) {
                sleepQuietly(config.heartbeatIntervalSeconds)
                if (heartbeatLoopRunning) {
                    sendHeartbeat()
                }
            }
        }
        thread.name = "authman-heartbeat"
        thread.isDaemon = true
        thread.start()
    }

    private fun startEventLoop() {
        if (eventLoopRunning) {
            return
        }
        eventLoopRunning = true
        val thread = Thread {
            var delaySeconds = config.websocketReconnectMinSeconds
            while (eventLoopRunning) {
                if (!config.websocketEnabled) {
                    sleepQuietly(config.heartbeatIntervalSeconds)
                    delaySeconds = config.websocketReconnectMinSeconds
                    continue
                }
                try {
                    logger.info("Connecting Authman node event stream")
                    client.connectEvents(
                        onSync = { applySync(it, "websocket") },
                        onRevoked = { lockFromCore("Core revoked this node token") },
                        onPresenceCheck = { downstreamListener.checkPresenceOverWebSocket(it) },
                    ).join()
                    delaySeconds = config.websocketReconnectMinSeconds
                } catch (ex: Exception) {
                    if (eventLoopRunning && config.websocketEnabled) {
                        logger.warn("Authman node event stream disconnected; REST heartbeat remains active", ex)
                    }
                }
                sleepQuietly(delaySeconds)
                delaySeconds = min(config.websocketReconnectMaxSeconds, (delaySeconds * 2).coerceAtLeast(config.websocketReconnectMinSeconds))
            }
        }
        thread.name = "authman-node-events"
        thread.isDaemon = true
        thread.start()
    }

    private fun sleepQuietly(seconds: Long) {
        try {
            Thread.sleep(seconds.coerceAtLeast(1) * 1000L)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
    }

    fun isCoreAccessRevoked(): Boolean = coreAccessRevoked

    fun onlinePlayerNames(): List<String> =
        server.allPlayers.map { it.username }.sortedWith(String.CASE_INSENSITIVE_ORDER)

    fun downstreamTransferSuggestions(prefix: String): List<String> {
        val clean = prefix.trim()
        val suggestions = LinkedHashSet<String>()
        for (target in downstreamTransferTargets) {
            listOf(target.slug, target.id, target.displayName)
                .map { it.trim() }
                .filter { it.isNotBlank() && !it.contains(' ') }
                .filter { clean.isBlank() || it.startsWith(clean, ignoreCase = true) }
                .forEach { suggestions += it }
        }
        return suggestions.toList().take(25)
    }

    fun transferPlayer(playerName: String, targetRef: String): String {
        if (coreAccessRevoked) {
            throw IllegalStateException("Authman Core connection is revoked")
        }
        val player = server.getPlayer(playerName).orElse(null)
            ?: server.allPlayers.firstOrNull {
                it.username.equals(playerName, ignoreCase = true) || it.uniqueId.toString().equals(playerName, ignoreCase = true)
            }
            ?: throw IllegalArgumentException("player is not online: $playerName")
        val target = resolveTransferTarget(targetRef)
        val targetID = target?.id ?: targetRef.trim()
        if (targetID.isBlank()) {
            throw IllegalArgumentException("target downstream server is required")
        }
        val remoteIp = player.remoteAddress.address?.hostAddress ?: player.remoteAddress.hostString
        val result = client.createDownstreamTransfer(
            playerId = player.uniqueId.toString(),
            username = player.username,
            serverId = targetID,
            source = config.serverId.ifBlank { config.nodeName },
            remoteIp = remoteIp,
            protocolVersion = player.protocolVersion.protocol,
        )
        val host = result.target.transferHost.trim()
        val port = result.target.transferPort
        if (host.isBlank() || port !in 1..65535) {
            throw IllegalStateException("Core returned invalid transfer target $host:$port")
        }
        val address = InetSocketAddress.createUnresolved(host, port)
        try {
            player.storeCookie(Key.key(config.transferCookieKey), result.token.toByteArray(StandardCharsets.UTF_8))
        } catch (ex: IllegalArgumentException) {
            throw IllegalStateException("client does not support transfer cookies; Minecraft 1.20.5+ is required", ex)
        }
        server.scheduler.buildTask(this, Runnable {
            if (!player.isActive) {
                return@Runnable
            }
            try {
                player.transferToHost(address)
            } catch (ex: IllegalArgumentException) {
                logger.warn("Failed to transfer {} to {}:{} after storing Authman grant", player.username, host, port, ex)
                player.sendMessage(Component.text("Authman transfer failed: Minecraft 1.20.5+ is required.", NamedTextColor.RED))
            }
        }).delay(200, TimeUnit.MILLISECONDS).schedule()
        val label = target?.displayName?.takeIf { it.isNotBlank() } ?: result.target.serverId.ifBlank { targetID }
        return "Transferring ${player.username} to $label ($host:$port)."
    }

    fun renameCurrentProfile(playerName: String, protocolName: String): String {
        if (coreAccessRevoked) {
            throw IllegalStateException("Authman Core connection is revoked")
        }
        val player = server.getPlayer(playerName).orElse(null)
            ?: server.allPlayers.firstOrNull {
                it.username.equals(playerName, ignoreCase = true) || it.uniqueId.toString().equals(playerName, ignoreCase = true)
            }
            ?: throw IllegalArgumentException("player is not online: $playerName")
        val remoteIp = player.remoteAddress.address?.hostAddress ?: player.remoteAddress.hostString
        val nextName = client.renameProfile(
            profileId = player.uniqueId.toString(),
            protocolName = protocolName,
            remoteIp = remoteIp,
        )
        return "Renamed ${player.username}'s current Authman profile to $nextName. The player must leave and rejoin before downstream servers see the new name."
    }

    private fun resolveTransferTarget(ref: String): DownstreamServerOption? {
        val clean = ref.trim()
        if (clean.isBlank()) {
            return null
        }
        return downstreamTransferTargets.firstOrNull {
            it.id.equals(clean, ignoreCase = true) ||
                it.slug.equals(clean, ignoreCase = true) ||
                it.displayName.equals(clean, ignoreCase = true)
        }
    }

    fun lockFromCore(reason: String) {
        if (!coreAccessRevoked) {
            logger.warn("Locking Authman Velocity node: {}", reason)
        }
        coreAccessRevoked = true
        server.allPlayers.forEach { player ->
            if (player.isActive) {
                player.disconnect(messages.temporaryUnavailable())
            }
        }
    }

    fun lockIfCoreRejected(error: Throwable): Boolean {
        val message = error.message ?: return false
        val rejected = message.contains("node.revoked") ||
            message.contains("node.unauthorized") ||
            message.contains("node token is invalid") ||
            message.contains("invalid node token")
        if (rejected) {
            lockFromCore("Core rejected this node token")
        }
        return rejected
    }

    private fun executeNodeActions(actions: List<NodeAction>): List<NodeActionAck> {
        if (actions.isEmpty()) {
            return emptyList()
        }
        val acked = mutableListOf<NodeActionAck>()
        for (action in actions) {
            when (action.type.lowercase()) {
                "disconnect" -> {
                    val count = downstreamListener.disconnectActionTargets(action)
                    acked += NodeActionAck(id = action.id, type = action.type, presenceId = action.presenceId)
                    logger.info(
                        "Executed Authman node action {} type={} disconnected={} profile={} presence={}",
                        action.id,
                        action.type,
                        count,
                        action.profileId,
                        action.presenceId,
                    )
                }
                "presence_check" -> {
                    acked += downstreamListener.checkPresenceAction(action)
                }
                else -> logger.warn("Ignoring unknown Authman node action {} type={}", action.id, action.type)
            }
        }
        return acked
    }

    companion object {
        const val VERSION = "0.1.0-dev"
    }
}
