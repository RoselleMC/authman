package mc.roselle.authman

import com.google.inject.Inject
import com.github.retrooper.packetevents.PacketEvents
import com.github.retrooper.packetevents.event.PacketListenerPriority
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent
import com.velocitypowered.api.plugin.Plugin
import com.velocitypowered.api.proxy.ProxyServer
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.config.InstanceFingerprint
import mc.roselle.authman.config.RuntimeMode
import mc.roselle.authman.dialog.DialogAuthView
import mc.roselle.authman.listener.GateAuthListener
import mc.roselle.authman.listener.PortalAuthListener
import mc.roselle.authman.message.AuthmanMessages
import mc.roselle.authman.session.AuthSessionStore
import org.slf4j.Logger
import java.nio.file.Path
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
    private lateinit var sessions: AuthSessionStore
    private lateinit var messages: AuthmanMessages

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = AuthmanConfig.load(dataDirectory)
        instanceFingerprint = InstanceFingerprint.load(dataDirectory)
        client = AuthmanClient(config, instanceFingerprint)
        sessions = AuthSessionStore(config)
        messages = AuthmanMessages(config)

        when (config.runtimeMode) {
            RuntimeMode.PORTAL -> {
                val listener = PortalAuthListener(this, server, logger, config, client, sessions, messages, DialogAuthView())
                server.eventManager.register(this, listener)
                PacketEvents.getAPI().eventManager.registerListener(listener, PacketListenerPriority.NORMAL)
            }
            RuntimeMode.GATE -> {
                server.eventManager.register(this, GateAuthListener(this, server, logger, config, client, messages))
            }
        }

        logger.info(
            "Authman plugin enabled in {} mode for {} with API {} instance={}",
            config.runtimeMode.name.lowercase(),
            server.version.name,
            config.apiBase,
            instanceFingerprint,
        )
        sendHeartbeat()
        server.scheduler.buildTask(this, Runnable { sendHeartbeat() })
            .repeat(config.heartbeatIntervalSeconds, TimeUnit.SECONDS)
            .schedule()
    }

    private fun sendHeartbeat() {
        try {
            val response = client.heartbeat(
                nodeName = config.nodeName,
                serverId = config.serverId,
                pluginVersion = VERSION,
                velocityVersion = server.version.version,
            )
            if (!response.ok) {
                logger.warn("Authman heartbeat failed with HTTP {}: {}", response.statusCode, response.body)
            }
        } catch (ex: Exception) {
            logger.warn("Authman heartbeat failed", ex)
        }
    }

    companion object {
        const val VERSION = "0.1.0-dev"
    }
}
