package mc.roselle.authman.listener

import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.DisconnectEvent
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.player.CookieReceiveEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.event.player.PlayerChooseInitialServerEvent
import com.velocitypowered.api.event.player.ServerConnectedEvent
import com.velocitypowered.api.event.player.ServerPreConnectEvent
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.proxy.ProxyServer
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.message.AuthmanMessages
import net.kyori.adventure.key.Key
import org.slf4j.Logger
import java.nio.charset.StandardCharsets
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

class GateAuthListener(
    private val plugin: Any,
    private val server: ProxyServer,
    private val logger: Logger,
    private val config: AuthmanConfig,
    private val client: AuthmanClient,
    private val messages: AuthmanMessages,
) {
    private val pending: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val allowed: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val validating: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val cookieKey: Key = Key.key(config.transferCookieKey)

    @Subscribe
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        val resolved = try {
            client.resolvePlayer(event.username)
        } catch (ex: Exception) {
            logger.warn("Failed to resolve Authman gate profile {}", event.username, ex)
            return
        }
        val profile = event.gameProfile
            .withId(resolved.uuid)
            .withName(resolved.protocolName)
            .withProperties(resolved.properties)
        event.setGameProfile(profile)
        logger.info("Applied Authman gate identity for {} as {} / {}", event.username, resolved.protocolName, resolved.uuid)
    }

    @Subscribe
    fun onLogin(event: LoginEvent) {
        val player = event.player
        pending.add(player.uniqueId)
        requestGrantCookie(player)
        scheduleValidationTimeout(player)
    }

    @Subscribe
    fun onCookieReceive(event: CookieReceiveEvent) {
        if (event.originalKey != cookieKey) {
            return
        }
        event.result = CookieReceiveEvent.ForwardResult.handled()
        val player = event.player
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
            validating.remove(player.uniqueId)
            reject(player, "invalid Authman transfer grant: ${ex.message}")
            return
        }
        allowed.add(player.uniqueId)
        validating.remove(player.uniqueId)
        pending.remove(player.uniqueId)
        logger.info("Accepted Authman gate grant for {} / {}", result.resolved.protocolName, result.resolved.uuid)
        scheduleInitialConnect(player, 250)
    }

    @Subscribe
	fun onChooseInitialServer(event: PlayerChooseInitialServerEvent) {
		val player = event.player
		if (!allowed.contains(player.uniqueId)) {
			requestGrantCookie(player)
            val holding = config.gateHoldingServer
            if (holding.isBlank()) {
                event.setInitialServer(null)
                return
            }
            event.setInitialServer(server.getServer(holding).orElse(null))
			return
		}
		val configured = config.gateInitialServer
		if (configured.isNotBlank()) {
			event.setInitialServer(server.getServer(configured).orElse(event.initialServer.orElse(null)))
		}
	}

    @Subscribe
	fun onServerPreConnect(event: ServerPreConnectEvent) {
		if (!allowed.contains(event.player.uniqueId)) {
            val holding = config.gateHoldingServer
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
    }

    @Subscribe
    fun onServerConnected(event: ServerConnectedEvent) {
        val holding = config.gateHoldingServer
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
        val configured = config.gateInitialServer
        if (configured.isBlank()) {
            return
        }
        val target = server.getServer(configured).orElse(null) ?: return
        if (player.currentServer.map { it.server.serverInfo.name == configured }.orElse(false)) {
            return
        }
        player.createConnectionRequest(target).connect().whenComplete { result, error ->
            if (error != null) {
                logger.warn("Failed to connect Authman gate player {} to {}", player.username, configured, error)
                return@whenComplete
            }
            if (result.status == com.velocitypowered.api.proxy.ConnectionRequestBuilder.Status.CONNECTION_IN_PROGRESS) {
                scheduleInitialConnect(player, 500)
                return@whenComplete
            }
            if (!result.isSuccessful) {
                logger.warn("Authman gate player {} was not connected to {}: {}", player.username, configured, result.status)
            }
        }
    }

    private fun requestGrantCookie(player: Player) {
        try {
            player.requestCookie(cookieKey)
        } catch (ex: IllegalArgumentException) {
            reject(player, "client does not support transfer cookies")
        }
    }

    private fun scheduleValidationTimeout(player: Player) {
        server.scheduler.buildTask(plugin, Runnable {
            if (player.isActive && pending.contains(player.uniqueId) && !allowed.contains(player.uniqueId)) {
                reject(player, "Authman gate validation timed out")
            }
        }).delay(config.gateValidationTimeoutSeconds, TimeUnit.SECONDS).schedule()
    }

    private fun reject(player: Player, reason: String) {
        pending.remove(player.uniqueId)
        allowed.remove(player.uniqueId)
        validating.remove(player.uniqueId)
        logger.info("Rejected Authman gate player {}: {}", player.username, reason)
        if (player.isActive) {
            player.disconnect(messages.temporaryUnavailable())
        }
    }
}
