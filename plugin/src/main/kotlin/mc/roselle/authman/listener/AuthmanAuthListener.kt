package mc.roselle.authman.listener

import com.velocitypowered.api.event.ResultedEvent
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.DisconnectEvent
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.event.player.PlayerChatEvent
import com.velocitypowered.api.event.player.ServerConnectedEvent
import com.velocitypowered.api.event.player.ServerPreConnectEvent
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.proxy.ProxyServer
import com.velocitypowered.api.proxy.server.RegisteredServer
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.message.AuthmanMessages
import mc.roselle.authman.model.PlayerAuthState
import mc.roselle.authman.model.ResolvedPlayer
import mc.roselle.authman.session.AuthSessionStore
import org.slf4j.Logger
import java.time.Instant
import java.util.concurrent.TimeUnit

class AuthmanAuthListener(
    private val plugin: Any,
    private val server: ProxyServer,
    private val logger: Logger,
    private val config: AuthmanConfig,
    private val client: AuthmanClient,
    private val sessions: AuthSessionStore,
    private val messages: AuthmanMessages,
) {
    @Subscribe
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        val username = event.username
        if (!shouldResolve(username)) {
            return
        }
        val resolved = try {
            client.resolvePlayer(username)
        } catch (ex: Exception) {
            if (username.startsWith("#")) {
                logger.warn("Failed to resolve Authman offline player {}", username, ex)
            }
            return
        }
        val profile = event.gameProfile
            .withId(resolved.uuid)
            .withName(resolved.protocolName)
            .withProperties(resolved.properties)
        event.setGameProfile(profile)
        sessions.rememberProfile(resolved.uuid, resolved)
        logger.info("Applied Authman identity for {} as {} / {} locked={}", username, resolved.protocolName, resolved.uuid, resolved.locked)
    }

    @Subscribe
    fun onLogin(event: LoginEvent) {
        val session = sessions.get(event.player.uniqueId) ?: return
        if (session.resolved.locked || session.state == PlayerAuthState.LOCKED) {
            event.setResult(ResultedEvent.ComponentResult.denied(messages.locked()))
            logger.info("Denied locked Authman account {}", event.player.username)
        }
    }

    @Subscribe
    fun onServerPreConnect(event: ServerPreConnectEvent) {
        val player = event.player
        val session = sessions.get(player.uniqueId) ?: return
        if (!session.resolved.authRequired || sessions.isAuthenticated(player.uniqueId)) {
            return
        }
        if (session.state == PlayerAuthState.LOCKED || session.resolved.locked) {
            event.setResult(ServerPreConnectEvent.ServerResult.denied())
            player.disconnect(messages.locked())
            logger.info("Blocked locked Authman account {} before connecting to {}", player.username, event.originalServer.serverInfo.name)
            return
        }

        val holding = holdingServerFor(player, event.originalServer)
        val current = player.currentServer.map { it.server }.orElse(null)
        if (current == null) {
            if (holding != event.originalServer) {
                sessions.markPending(player.uniqueId, targetAfterAuth(event.originalServer))
                event.setResult(ServerPreConnectEvent.ServerResult.allowed(holding))
            }
            return
        }

        sessions.markPending(player.uniqueId, targetAfterAuth(event.originalServer))
        event.setResult(ServerPreConnectEvent.ServerResult.allowed(current))
        if (sessions.shouldPrompt(player.uniqueId, Instant.now())) {
            messages.sendPasswordPrompt(player, session)
        }
        logger.info("Held unauthenticated Authman player {} on {} while target is {}", player.username, current.serverInfo.name, event.originalServer.serverInfo.name)
    }

    @Subscribe
    fun onServerConnected(event: ServerConnectedEvent) {
        val player = event.player
        val session = sessions.get(player.uniqueId) ?: return
        if (!session.resolved.authRequired || sessions.isAuthenticated(player.uniqueId)) {
            return
        }
        if (sessions.shouldPrompt(player.uniqueId, Instant.now(), force = true)) {
            messages.sendPasswordPrompt(player, session)
        }
        scheduleTimeoutCheck(player)
        logger.info("Waiting for Authman password authentication for {}", player.username)
    }

    @Subscribe
    fun onPlayerChat(event: PlayerChatEvent) {
        val player = event.player
        val session = sessions.get(player.uniqueId) ?: return
        if (!session.resolved.authRequired || sessions.isAuthenticated(player.uniqueId)) {
            return
        }
        event.setResult(PlayerChatEvent.ChatResult.denied())
        val now = Instant.now()
        if (!sessions.canAcceptChat(player.uniqueId, now)) {
            messages.sendCooldownIgnored(player)
            return
        }
        val password = event.message.trim()
        if (password.isEmpty()) {
            session.lastInputMarker = "/empty"
            messages.sendPasswordPrompt(player, session)
            return
        }
        val result = try {
            client.authenticatePlayer(session.resolved.protocolName, password)
        } catch (ex: Exception) {
            logger.warn("Failed to authenticate Authman player {}", player.username, ex)
            messages.sendTemporaryUnavailable(player)
            return
        }
        if (result.locked) {
            sessions.markLocked(player.uniqueId)
            player.disconnect(messages.locked())
            logger.info("Disconnected locked Authman account {} during password authentication", player.username)
            return
        }
        if (!result.authenticated) {
            val wrong = sessions.markWrongPassword(player.uniqueId)
            if (wrong >= config.maxPasswordAttempts) {
                sessions.clear(player.uniqueId)
                player.disconnect(messages.tooManyWrongPasswords())
                logger.info("Disconnected Authman account {} after {} wrong password attempts", player.username, wrong)
                return
            }
            messages.sendPasswordPrompt(player, session)
            return
        }
        val pending = session.pendingServer
        sessions.markAuthenticated(player.uniqueId)
        messages.sendSuccess(player)
        connectAfterAuth(player, pending)
        logger.info("Authenticated Authman offline account {}", player.username)
    }

    @Subscribe
    fun onDisconnect(event: DisconnectEvent) {
        sessions.clear(event.player.uniqueId)
    }

    private fun shouldResolve(username: String): Boolean {
        return username.startsWith("#") || config.resolveRawOfflineNames
    }

    private fun holdingServerFor(player: Player, fallback: RegisteredServer): RegisteredServer {
        if (config.holdingServer.isNotBlank()) {
            return server.getServer(config.holdingServer).orElse(fallback)
        }
        return player.currentServer.map { it.server }.orElse(fallback)
    }

    private fun targetAfterAuth(requested: RegisteredServer): RegisteredServer {
        if (config.defaultTargetServer.isNotBlank()) {
            return server.getServer(config.defaultTargetServer).orElse(requested)
        }
        return requested
    }

    private fun connectAfterAuth(player: Player, pending: RegisteredServer?) {
        val target = pending ?: config.defaultTargetServer
            .takeIf { it.isNotBlank() }
            ?.let { server.getServer(it).orElse(null) }
            ?: return
        server.scheduler.buildTask(plugin, Runnable {
            if (player.isActive) {
                player.createConnectionRequest(target).connect()
            }
        }).delay(config.completionDelaySeconds, TimeUnit.SECONDS).schedule()
    }

    private fun scheduleTimeoutCheck(player: Player) {
        server.scheduler.buildTask(plugin, Runnable {
            val session = sessions.get(player.uniqueId) ?: return@Runnable
            if (session.state == PlayerAuthState.WAITING_PASSWORD && !sessions.isAuthenticated(player.uniqueId) && player.isActive) {
                sessions.clear(player.uniqueId)
                player.disconnect(messages.authTimeout())
                logger.info("Disconnected Authman player {} after auth timeout", player.username)
            }
        }).delay(config.authTimeoutSeconds, TimeUnit.SECONDS).schedule()
    }
}
