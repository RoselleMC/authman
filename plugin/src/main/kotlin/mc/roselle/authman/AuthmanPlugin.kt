package mc.roselle.authman

import com.google.inject.Inject
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent
import com.velocitypowered.api.plugin.Plugin
import com.velocitypowered.api.proxy.ProxyServer
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.command.AuthmanCommand
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.config.InstanceFingerprint
import mc.roselle.authman.listener.DownstreamAuthListener
import mc.roselle.authman.message.AuthmanMessages
import mc.roselle.authman.model.DownstreamServerOption
import mc.roselle.authman.model.NodeAction
import net.kyori.adventure.key.Key
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import org.slf4j.Logger
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.nio.file.Path
import java.util.UUID
import java.util.concurrent.TimeUnit

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

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = AuthmanConfig.load(dataDirectory)
        instanceFingerprint = InstanceFingerprint.load(dataDirectory)
        client = AuthmanClient(config, instanceFingerprint)
        messages = AuthmanMessages()

        server.eventManager.register(this, DownstreamAuthListener(this, server, logger, config, client, messages))
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
        server.scheduler.buildTask(this, Runnable { sendHeartbeat() })
            .repeat(config.heartbeatIntervalSeconds, TimeUnit.SECONDS)
            .schedule()
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
            val response = client.heartbeat(pluginVersion = VERSION, velocityVersion = server.version.version)
            if (!response.ok) {
                logger.warn("Authman heartbeat failed with HTTP {}: {}", response.statusCode, response.body)
                if (response.accessRevoked) {
                    lockFromCore("Core revoked or rejected this node token")
                }
                return false
            }
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
                logger.info("Authman Core access restored after successful heartbeat")
            }
            coreAccessRevoked = false
            return true
        } catch (ex: Exception) {
            logger.warn("Authman heartbeat failed", ex)
            return false
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

    private fun executeNodeActions(actions: List<NodeAction>): List<String> {
        if (actions.isEmpty()) {
            return emptyList()
        }
        val acked = mutableListOf<String>()
        for (action in actions) {
            when (action.type.lowercase()) {
                "disconnect" -> {
                    disconnectActionTargets(action)
                    acked += action.id
                }
                else -> logger.warn("Ignoring unknown Authman node action {} type={}", action.id, action.type)
            }
        }
        return acked
    }

    private fun disconnectActionTargets(action: NodeAction) {
        val uuid = runCatching {
            action.uuid.takeIf { it.isNotBlank() }?.let(UUID::fromString)
        }.getOrNull()
        val component = if (action.reason.isBlank()) {
            messages.defaultDisconnect(action.protocolName)
        } else {
            Component.text(action.reason, NamedTextColor.RED)
        }
        var count = 0
        server.allPlayers.forEach { player ->
            val uuidMatches = uuid != null && player.uniqueId == uuid
            val nameMatches = action.protocolName.isNotBlank() && player.username.equals(action.protocolName, ignoreCase = true)
            if (player.isActive && (uuidMatches || nameMatches)) {
                player.disconnect(component)
                count++
            }
        }
        logger.info(
            "Executed Authman node action {} type={} disconnected={} profile={} presence={}",
            action.id,
            action.type,
            count,
            action.profileId,
            action.presenceId,
        )
    }

    companion object {
        const val VERSION = "0.1.0-dev"
    }
}
