package mc.roselle.authman.listener

import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.DisconnectEvent
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.connection.PostLoginEvent
import com.velocitypowered.api.event.player.CookieReceiveEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.event.player.PlayerChooseInitialServerEvent
import com.velocitypowered.api.event.player.ServerConnectedEvent
import com.velocitypowered.api.event.player.ServerPreConnectEvent
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.proxy.ProxyServer
import mc.roselle.authman.AuthmanPlugin
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.message.AuthmanMessages
import net.kyori.adventure.key.Key
import org.slf4j.Logger
import java.nio.charset.StandardCharsets
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

class DownstreamAuthListener(
    private val plugin: AuthmanPlugin,
    private val server: ProxyServer,
    private val logger: Logger,
    private val config: AuthmanConfig,
    private val client: AuthmanClient,
    private val messages: AuthmanMessages,
) {
    private val pending: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val allowed: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val validating: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val presences: MutableMap<UUID, String> = ConcurrentHashMap()

    @Subscribe
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        if (plugin.isCoreAccessRevoked()) {
            return
        }
        val resolved = try {
            client.resolvePlayer(event.username)
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            logger.warn("Failed to resolve Authman downstream profile {}", event.username, ex)
            return
        }
        val profile = event.gameProfile
            .withId(resolved.uuid)
            .withName(resolved.protocolName)
            .withProperties(resolved.properties)
        event.setGameProfile(profile)
        logger.info("Applied Authman downstream identity for {} as {} / {}", event.username, resolved.protocolName, resolved.uuid)
    }

    @Subscribe
    fun onLogin(event: LoginEvent) {
        if (plugin.isCoreAccessRevoked()) {
            event.setResult(com.velocitypowered.api.event.ResultedEvent.ComponentResult.denied(messages.temporaryUnavailable()))
            return
        }
        val player = event.player
        logger.info("Authman downstream LoginEvent identity for {} / {}", player.username, player.uniqueId)
        pending.add(player.uniqueId)
        requestGrantCookie(player)
        scheduleValidationTimeout(player)
    }

    @Subscribe
    fun onPostLogin(event: PostLoginEvent) {
        val player = event.player
        logger.info("Authman downstream PostLoginEvent identity for {} / {}", player.username, player.uniqueId)
    }

    @Subscribe
    fun onCookieReceive(event: CookieReceiveEvent) {
        if (event.originalKey != cookieKey()) {
            return
        }
        event.result = CookieReceiveEvent.ForwardResult.handled()
        val player = event.player
        if (plugin.isCoreAccessRevoked()) {
            reject(player, "Authman node access is revoked")
            return
        }
        if (allowed.contains(player.uniqueId)) {
            return
        }
        if (!validating.add(player.uniqueId)) {
            return
        }
        val data = event.originalData
        if (data == null || data.isEmpty()) {
            validating.remove(player.uniqueId)
            reject(player, "missing Authman transfer grant cookie")
            return
        }
        val token = data.toString(StandardCharsets.UTF_8).trim()
        if (token.isEmpty()) {
            validating.remove(player.uniqueId)
            reject(player, "empty Authman transfer grant cookie")
            return
        }
        val source = player.remoteAddress.address?.hostAddress ?: player.remoteAddress.hostString
        val result = try {
            client.consumeTransferGrant(
                token = token,
                serverId = config.serverId,
                uuid = player.uniqueId.toString(),
                protocolName = player.username,
                source = source,
            )
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            validating.remove(player.uniqueId)
            reject(player, "invalid Authman transfer grant: ${ex.message}")
            return
        }
        allowed.add(player.uniqueId)
        if (result.presenceId.isNotBlank()) {
            presences[player.uniqueId] = result.presenceId
        }
        validating.remove(player.uniqueId)
        pending.remove(player.uniqueId)
        logger.info("Accepted Authman downstream grant for {} / {}", result.resolved.protocolName, result.resolved.uuid)
        scheduleInitialConnect(player, 250)
    }

    @Subscribe
    fun onChooseInitialServer(event: PlayerChooseInitialServerEvent) {
        val player = event.player
        if (plugin.isCoreAccessRevoked()) {
            event.setInitialServer(null)
            return
        }
        if (!allowed.contains(player.uniqueId)) {
            requestGrantCookie(player)
            val holding = config.downstreamHoldingServer
            if (holding.isNotBlank()) {
                event.setInitialServer(server.getServer(holding).orElse(null))
                return
            }
            event.setInitialServer(resolveInitialServer(event))
            return
        }
        event.setInitialServer(resolveInitialServer(event))
    }

    @Subscribe
	fun onServerPreConnect(event: ServerPreConnectEvent) {
        if (plugin.isCoreAccessRevoked()) {
            event.setResult(ServerPreConnectEvent.ServerResult.denied())
            event.player.disconnect(messages.temporaryUnavailable())
            return
        }
		if (!allowed.contains(event.player.uniqueId)) {
            val holding = config.downstreamHoldingServer
            if (holding.isNotBlank() && event.originalServer.serverInfo.name == holding) {
                requestGrantCookie(event.player)
                return
            }
            event.setResult(ServerPreConnectEvent.ServerResult.denied())
            requestGrantCookie(event.player)
		}
	}

    @Subscribe
    fun onDisconnect(event: DisconnectEvent) {
        pending.remove(event.player.uniqueId)
        allowed.remove(event.player.uniqueId)
        validating.remove(event.player.uniqueId)
        val presenceId = presences.remove(event.player.uniqueId)
        if (!presenceId.isNullOrBlank()) {
            server.scheduler.buildTask(plugin, Runnable {
                try {
                    client.endPresence(presenceId, "disconnect")
                } catch (ex: Exception) {
                    plugin.lockIfCoreRejected(ex)
                    logger.warn("Failed to end Authman presence {} for {}", presenceId, event.player.username, ex)
                }
            }).schedule()
        }
    }

    @Subscribe
    fun onServerConnected(event: ServerConnectedEvent) {
        val holding = config.downstreamHoldingServer
        if (holding.isNotBlank() && allowed.contains(event.player.uniqueId) && event.server.serverInfo.name == holding) {
            scheduleInitialConnect(event.player, 250)
        }
    }

    private fun scheduleInitialConnect(player: Player, delayMillis: Long) {
        server.scheduler.buildTask(plugin, Runnable {
            connectInitialServer(player)
        }).delay(delayMillis.coerceAtLeast(0), TimeUnit.MILLISECONDS).schedule()
    }

    private fun connectInitialServer(player: Player) {
        if (!player.isActive) {
            return
        }
        val target = resolveInitialServer(null) ?: return
        val targetName = target.serverInfo.name
        if (player.currentServer.map { it.server.serverInfo.name == targetName }.orElse(false)) {
            return
        }
        player.createConnectionRequest(target).connect().whenComplete { result, error ->
            if (error != null) {
                logger.warn("Failed to connect Authman downstream player {} to {}", player.username, targetName, error)
                return@whenComplete
            }
            if (result.status == com.velocitypowered.api.proxy.ConnectionRequestBuilder.Status.CONNECTION_IN_PROGRESS) {
                scheduleInitialConnect(player, 500)
                return@whenComplete
            }
            if (!result.isSuccessful) {
                logger.warn("Authman downstream player {} was not connected to {}: {}", player.username, targetName, result.status)
            }
        }
    }

    private fun resolveInitialServer(event: PlayerChooseInitialServerEvent?): com.velocitypowered.api.proxy.server.RegisteredServer? {
        val configured = config.downstreamInitialServer
        if (configured.isNotBlank()) {
            val configuredServer = server.getServer(configured).orElse(null)
            if (configuredServer != null) {
                return configuredServer
            }
            logger.warn("Configured Authman downstream initial server {} is not registered in Velocity", configured)
        }
        val eventInitial = event?.initialServer?.orElse(null)
        if (eventInitial != null) {
            return eventInitial
        }
        return server.allServers.firstOrNull()
    }

    private fun requestGrantCookie(player: Player) {
        try {
            player.requestCookie(cookieKey())
        } catch (ex: IllegalArgumentException) {
            reject(player, "client does not support transfer cookies")
        }
    }

    private fun cookieKey(): Key = Key.key(config.transferCookieKey)

    private fun scheduleValidationTimeout(player: Player) {
        server.scheduler.buildTask(plugin, Runnable {
            if (player.isActive && pending.contains(player.uniqueId) && !allowed.contains(player.uniqueId)) {
                reject(player, "Authman downstream validation timed out")
            }
        }).delay(config.downstreamValidationTimeoutSeconds, TimeUnit.SECONDS).schedule()
    }

    private fun reject(player: Player, reason: String) {
        pending.remove(player.uniqueId)
        allowed.remove(player.uniqueId)
        validating.remove(player.uniqueId)
        logger.info("Rejected Authman downstream player {}: {}", player.username, reason)
        if (player.isActive) {
            player.disconnect(messages.temporaryUnavailable())
        }
    }
}
