package com.iroselle.authman

import com.iroselle.authman.api.AuthmanClient
import com.iroselle.authman.api.handleRuntimeNodeMessage
import com.iroselle.authman.api.parseRuntimeSync
import com.iroselle.authman.command.AuthmanCommand
import com.iroselle.authman.config.AuthmanConfig
import com.iroselle.authman.listener.DownstreamAuthListener
import com.iroselle.authman.message.AuthmanMessages
import com.iroselle.authman.model.DownstreamServerOption
import com.iroselle.authman.model.NodeAction
import com.iroselle.authman.model.NodeActionAck
import com.iroselle.authman.model.PortalLinkResult
import com.iroselle.authman.spi.AuthmanRuntimeContext
import com.iroselle.authman.spi.AuthmanRuntimeModule
import com.iroselle.authman.spi.AuthmanRuntimeStatus
import com.iroselle.authman.spi.AuthmanCommandRegistration
import com.velocitypowered.api.proxy.ProxyServer
import com.velocitypowered.api.scheduler.ScheduledTask
import com.velocitypowered.api.scheduler.TaskStatus
import net.kyori.adventure.key.Key
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import org.slf4j.Logger
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.function.Consumer

class AuthmanPlugin : AuthmanRuntimeModule {
    private lateinit var context: AuthmanRuntimeContext
    private lateinit var server: ProxyServer
    private lateinit var logger: Logger
    private lateinit var config: AuthmanConfig
    private lateinit var client: AuthmanClient
    private lateinit var messages: AuthmanMessages
    @Volatile
    private var downstreamTransferTargets: List<DownstreamServerOption> = emptyList()
    private lateinit var downstreamListener: DownstreamAuthListener
    private var commandRegistration: AuthmanCommandRegistration? = null
    private var listenerRegistered = false
    private val scheduledTasks = ConcurrentHashMap.newKeySet<ScheduledTask>()
    @Volatile
    private var started = false

    override fun start(context: AuthmanRuntimeContext, previousState: ByteArray?) {
        check(!started) { "Authman runtime is already started" }
        this.context = context
        server = context.server
        logger = context.logger
        config = AuthmanConfig()
        client = AuthmanClient(context.transport)
        messages = AuthmanMessages()

        downstreamListener = DownstreamAuthListener(this, server, logger, config, client, messages)
        downstreamListener.importState(previousState)
        started = true
        try {
            server.eventManager.register(context.pluginOwner, downstreamListener)
            listenerRegistered = true
            commandRegistration = context.commands.install(AuthmanCommand(this, logger))
        } catch (ex: Throwable) {
            stop()
            throw ex
        }
        logger.info("Authman downstream runtime {} started", VERSION)
    }

    override fun applySync(payload: String) {
        check(started) { "Authman runtime is not started" }
        applySync(parseRuntimeSync(payload))
    }

    override fun handleNodeMessage(payload: String): String? {
        check(started) { "Authman runtime is not started" }
        return handleRuntimeNodeMessage(payload) { downstreamListener.checkPresenceOverWebSocket(it) }
    }

    override fun status(): AuthmanRuntimeStatus =
        AuthmanRuntimeStatus(
            onlinePlayers = downstreamListener.onlinePresenceCount(),
            maxPlayers = server.configuration.showMaxPlayers,
        )

    override fun snapshot(): ByteArray? = if (::downstreamListener.isInitialized) downstreamListener.exportState() else null

    override fun stop() {
        if (!started) {
            return
        }
        started = false
        commandRegistration?.let { registration ->
            runCatching { registration.close() }
                .onFailure { logger.warn("Failed to unregister Authman runtime command capabilities", it) }
        }
        commandRegistration = null
        scheduledTasks.forEach { runCatching { it.cancel() } }
        scheduledTasks.clear()
        if (listenerRegistered && ::downstreamListener.isInitialized) {
            runCatching { server.eventManager.unregisterListener(context.pluginOwner, downstreamListener) }
                .onFailure { logger.warn("Failed to unregister Authman downstream runtime listener", it) }
            listenerRegistered = false
        }
        logger.info("Authman downstream runtime {} stopped", VERSION)
    }

    fun reconnectNow(): Boolean = context.control.reconnectNow()

    fun reloadConfigAndReconnect(): Boolean = context.control.reloadBootstrapConfigAndReconnect()

    fun client(): AuthmanClient = client

    private fun applySync(response: com.iroselle.authman.api.HeartbeatResult) {
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
    }

    fun isCoreAccessRevoked(): Boolean = context.control.isCoreAccessRevoked()

    fun schedule(task: Runnable, delay: Long = 0, unit: TimeUnit = TimeUnit.MILLISECONDS) {
        if (!started) {
            return
        }
        val scheduled = server.scheduler.buildTask(context.pluginOwner, Consumer<ScheduledTask> { current ->
            scheduledTasks.remove(current)
            if (started) {
                task.run()
            }
        }).delay(delay.coerceAtLeast(0), unit).schedule()
        scheduledTasks.add(scheduled)
        if (scheduled.status() != TaskStatus.SCHEDULED) {
            scheduledTasks.remove(scheduled)
        }
    }

    fun onlinePlayerNames(): List<String> =
        server.allPlayers.map { it.username }.sortedWith(String.CASE_INSENSITIVE_ORDER)

    fun commandStatus(): RuntimeCommandStatusView = RuntimeCommandStatusView(
        runtimeVersion = VERSION,
        nodeName = config.nodeName,
        serverId = config.serverId,
        onlinePlayers = downstreamListener.onlinePresenceCount(),
        maxPlayers = server.configuration.showMaxPlayers,
        transferTargets = downstreamTransferTargets.size,
    )

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
        if (isCoreAccessRevoked()) {
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
        schedule(Runnable {
            if (!player.isActive) {
                return@Runnable
            }
            try {
                player.transferToHost(address)
            } catch (ex: IllegalArgumentException) {
                logger.warn("Failed to transfer {} to {}:{} after storing Authman grant", player.username, host, port, ex)
                player.sendMessage(Component.text("Authman transfer failed: Minecraft 1.20.5+ is required.", NamedTextColor.RED))
            }
        }, 200, TimeUnit.MILLISECONDS)
        val label = target?.displayName?.takeIf { it.isNotBlank() } ?: result.target.serverId.ifBlank { targetID }
        return "Transferring ${player.username} to $label ($host:$port)."
    }

    fun renameCurrentProfile(playerName: String, protocolName: String): String {
        if (isCoreAccessRevoked()) {
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

    fun createPortalLink(playerName: String): PortalLinkResult {
        if (isCoreAccessRevoked()) {
            throw IllegalStateException("Authman Core connection is revoked")
        }
        val player = server.getPlayer(playerName).orElse(null)
            ?: server.allPlayers.firstOrNull {
                it.username.equals(playerName, ignoreCase = true) || it.uniqueId.toString().equals(playerName, ignoreCase = true)
            }
            ?: throw IllegalArgumentException("player is not online: $playerName")
        return client.createPortalLink(
            profileId = player.uniqueId.toString(),
            username = player.username,
            serverId = config.serverId,
        )
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
        if (!isCoreAccessRevoked()) {
            logger.warn("Locking Authman Velocity node: {}", reason)
        }
        context.control.lockCoreAccess(reason)
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
        const val VERSION = "0.3.0-dev"
    }
}

data class RuntimeCommandStatusView(
    val runtimeVersion: String,
    val nodeName: String,
    val serverId: String,
    val onlinePlayers: Int,
    val maxPlayers: Int,
    val transferTargets: Int,
)
