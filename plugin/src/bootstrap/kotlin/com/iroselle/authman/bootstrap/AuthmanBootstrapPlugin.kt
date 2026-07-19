package com.iroselle.authman.bootstrap

import com.google.gson.Gson
import com.google.gson.JsonObject
import com.google.inject.Inject
import com.iroselle.authman.spi.AUTHMAN_RUNTIME_API_VERSION
import com.iroselle.authman.spi.RuntimeControl
import com.velocitypowered.api.command.CommandMeta
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent
import com.velocitypowered.api.event.proxy.ProxyShutdownEvent
import com.velocitypowered.api.plugin.Plugin
import com.velocitypowered.api.proxy.ProxyServer
import org.slf4j.Logger
import java.nio.file.Path
import kotlin.math.min

@Plugin(
    id = "authman",
    name = "authman",
    version = AuthmanBootstrapPlugin.VERSION,
    description = "Stable Authman Velocity runtime bootstrap",
    authors = ["Score2"],
)
class AuthmanBootstrapPlugin @Inject constructor(
    private val server: ProxyServer,
    private val logger: Logger,
    @com.velocitypowered.api.plugin.annotation.DataDirectory private val dataDirectory: Path,
) : RuntimeControl {
    private val gson = Gson()

    @Volatile
    private var running = false

    @Volatile
    private var coreAccessRevoked = false

    @Volatile
    private var heartbeatIntervalSeconds = 60L

    @Volatile
    private var websocketEnabled = true

    @Volatile
    private var websocketReconnectMinSeconds = 2L

    @Volatile
    private var websocketReconnectMaxSeconds = 60L

    private lateinit var config: BootstrapConfig
    private lateinit var fingerprint: String
    private lateinit var transport: CoreTransport
    private lateinit var commandRouter: RuntimeCommandRouter
    private lateinit var runtimeManager: RuntimeManager
    private lateinit var bridgeChannel: BridgeChannel
    private var commandMeta: CommandMeta? = null

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = BootstrapConfig.load(dataDirectory)
        fingerprint = InstanceFingerprint.load(dataDirectory)
        transport = CoreTransport(config, fingerprint)
        commandRouter = RuntimeCommandRouter(logger, this)
        runtimeManager = RuntimeManager(
            server,
            logger,
            this,
            dataDirectory,
            transport,
            this,
            commandRouter,
            ::selectCommandMode,
        )
        bridgeChannel = BridgeChannel(server, logger, this, commandRouter)
        server.eventManager.register(this, bridgeChannel)
        server.eventManager.register(this, BootstrapGate(runtimeManager))

        running = true
        runtimeManager.loadCached()
        selectCommandMode(runtimeManager.activeApiVersion())
        sendHeartbeat()
        startHeartbeatLoop()
        startEventLoop()
        logger.info(
            "Authman stable bootstrap {} enabled for {} instance={} runtime-contract={}",
            VERSION,
            server.version.version,
            fingerprint,
            AUTHMAN_RUNTIME_API_VERSION,
        )
    }

    @Subscribe
    fun onProxyShutdown(event: ProxyShutdownEvent) {
        running = false
        if (::bridgeChannel.isInitialized) {
            bridgeChannel.close()
        }
        if (::runtimeManager.isInitialized) {
            runtimeManager.close()
        }
        if (::transport.isInitialized) {
            transport.close()
        }
        unregisterBootstrapCommand()
    }

    override fun reconnectNow(): Boolean = sendHeartbeat()

    override fun reloadBootstrapConfigAndReconnect(): Boolean {
        commandRouter.setCoreConnected(false)
        val next = BootstrapConfig.load(dataDirectory)
        config = next
        transport.replaceConfig(next)
        return sendHeartbeat()
    }

    override fun isCoreAccessRevoked(): Boolean = coreAccessRevoked

    override fun lockCoreAccess(reason: String) {
        if (!coreAccessRevoked) {
            logger.warn("Authman Core access locked by bootstrap: {}", reason)
        }
        coreAccessRevoked = true
        if (::commandRouter.isInitialized) {
            commandRouter.setCoreConnected(false)
        }
    }

    private fun sendHeartbeat(): Boolean {
        if (!::runtimeManager.isInitialized) {
            return false
        }
        return try {
            val status = runtimeManager.runtimeStatus()
            val body = gson.toJson(
                mapOf(
                    "kind" to "downstream_velocity",
                    "name" to server.version.name,
                    "instance_fingerprint" to fingerprint,
                    "plugin_version" to VERSION,
                    "bootstrap_api_version" to AUTHMAN_RUNTIME_API_VERSION,
                    "velocity_version" to server.version.version,
                    "status" to mapOf(
                        "online_players" to status.onlinePlayers,
                        "max_players" to status.maxPlayers,
                    ),
                    "runtime_module" to runtimeManager.report(),
                ),
            )
            val response = transport.heartbeat(body)
            if (!response.ok) {
                commandRouter.setCoreConnected(false)
                logger.warn("Authman heartbeat failed with HTTP {}: {}", response.statusCode, response.body.take(500))
                if (response.statusCode == 401 || response.statusCode == 403) {
                    lockCoreAccess("Core rejected the node token")
                }
                false
            } else {
                val root = gson.fromJson(response.body, JsonObject::class.java)
                applySync(root.getAsJsonObject("data") ?: JsonObject())
                if (coreAccessRevoked) {
                    logger.info("Authman Core access restored by heartbeat")
                }
                coreAccessRevoked = false
                commandRouter.setCoreConnected(true)
                true
            }
        } catch (ex: Exception) {
            commandRouter.setCoreConnected(false)
            logger.warn("Authman heartbeat failed", ex)
            false
        }
    }

    private fun applySync(data: JsonObject) {
        val communication = data.getAsJsonObject("runtime_config") ?: JsonObject()
        heartbeatIntervalSeconds = communication.longOr("heartbeat_interval_seconds", heartbeatIntervalSeconds).coerceIn(10, 600)
        websocketEnabled = communication.booleanOr("websocket_enabled", websocketEnabled)
        websocketReconnectMinSeconds = communication.longOr("websocket_reconnect_min_seconds", websocketReconnectMinSeconds).coerceIn(1, 300)
        websocketReconnectMaxSeconds = communication.longOr("websocket_reconnect_max_seconds", websocketReconnectMaxSeconds)
            .coerceIn(websocketReconnectMinSeconds, 900)
        runtimeManager.applySync(gson.toJson(data))
    }

    private fun onEventMessage(message: String): String? {
        val root = gson.fromJson(message, JsonObject::class.java)
        return when (root.stringOr("type", "")) {
            "sync" -> {
                applySync(root.getAsJsonObject("data") ?: JsonObject())
                coreAccessRevoked = false
                commandRouter.setCoreConnected(true)
                null
            }
            "revoked" -> {
                lockCoreAccess("Core revoked this node token")
                null
            }
            "ping" -> {
                coreAccessRevoked = false
                commandRouter.setCoreConnected(true)
                null
            }
            else -> runtimeManager.handleNodeMessage(message)
        }
    }

    private fun startHeartbeatLoop() {
        Thread({
            while (running) {
                sleepQuietly(heartbeatIntervalSeconds)
                if (running) {
                    sendHeartbeat()
                }
            }
        }, "authman-bootstrap-heartbeat").apply {
            isDaemon = true
            start()
        }
    }

    private fun startEventLoop() {
        Thread({
            var delaySeconds = websocketReconnectMinSeconds
            while (running) {
                if (!websocketEnabled) {
                    sleepQuietly(heartbeatIntervalSeconds)
                    delaySeconds = websocketReconnectMinSeconds
                    continue
                }
                try {
                    logger.info("Connecting Authman bootstrap event stream")
                    transport.connectEvents(::onEventMessage).join()
                    delaySeconds = websocketReconnectMinSeconds
                } catch (ex: Exception) {
                    if (running && websocketEnabled) {
                        logger.warn("Authman bootstrap event stream disconnected; heartbeat remains active", ex)
                    }
                }
                sleepQuietly(delaySeconds)
                delaySeconds = min(websocketReconnectMaxSeconds, (delaySeconds * 2).coerceAtLeast(websocketReconnectMinSeconds))
            }
        }, "authman-bootstrap-events").apply {
            isDaemon = true
            start()
        }
    }

    private fun sleepQuietly(seconds: Long) {
        try {
            Thread.sleep(seconds.coerceAtLeast(1) * 1000L)
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        }
    }

    @Synchronized
    private fun selectCommandMode(runtimeApiVersion: Int?) {
        if (RuntimeCompatibility.bootstrapOwnsCommand(runtimeApiVersion)) {
            registerBootstrapCommand()
        } else {
            unregisterBootstrapCommand()
        }
    }

    private fun registerBootstrapCommand() {
        if (commandMeta != null) {
            return
        }
        val meta = server.commandManager.metaBuilder("authman")
            .aliases("am")
            .plugin(this)
            .build()
        server.commandManager.register(meta, BootstrapCommand(commandRouter))
        commandMeta = meta
    }

    private fun unregisterBootstrapCommand() {
        val meta = commandMeta ?: return
        runCatching { server.commandManager.unregister(meta) }
            .onFailure { logger.warn("Failed to unregister Authman bootstrap command", it) }
        commandMeta = null
    }

    companion object {
        const val VERSION = "1.0.0"
    }
}

private fun JsonObject.stringOr(key: String, fallback: String): String =
    get(key)?.takeIf { !it.isJsonNull }?.asString ?: fallback

private fun JsonObject.longOr(key: String, fallback: Long): Long =
    get(key)?.takeIf { !it.isJsonNull }?.asLong ?: fallback

private fun JsonObject.booleanOr(key: String, fallback: Boolean): Boolean =
    get(key)?.takeIf { !it.isJsonNull }?.asBoolean ?: fallback
