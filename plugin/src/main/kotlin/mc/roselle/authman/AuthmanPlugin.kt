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
import mc.roselle.authman.model.NodeAction
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import org.slf4j.Logger
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

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = AuthmanConfig.load(dataDirectory)
        instanceFingerprint = InstanceFingerprint.load(dataDirectory)
        client = AuthmanClient(config, instanceFingerprint)
        messages = AuthmanMessages(config)

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
        val reason = action.reason.ifBlank { "Authman disconnected this session." }
        val component = Component.text(reason, NamedTextColor.RED)
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
