package mc.roselle.authman.listener

import com.github.retrooper.packetevents.event.PacketListener
import com.github.retrooper.packetevents.event.PacketReceiveEvent
import com.github.retrooper.packetevents.protocol.packettype.PacketType
import com.github.retrooper.packetevents.wrapper.play.client.WrapperPlayClientCustomClickAction
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
import mc.roselle.authman.AuthmanPlugin
import mc.roselle.authman.api.AuthmanClient
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.dialog.DialogActionType
import mc.roselle.authman.dialog.DialogAuthView
import mc.roselle.authman.message.AuthmanMessages
import mc.roselle.authman.model.PlayerAuthSession
import mc.roselle.authman.model.PlayerAuthState
import mc.roselle.authman.model.ResolvedPlayer
import mc.roselle.authman.session.AuthSessionStore
import net.kyori.adventure.key.Key
import org.slf4j.Logger
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.time.Instant
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

class PortalAuthListener(
    private val plugin: AuthmanPlugin,
    private val server: ProxyServer,
    private val logger: Logger,
    private val config: AuthmanConfig,
    private val client: AuthmanClient,
    private val sessions: AuthSessionStore,
    private val messages: AuthmanMessages,
    private val dialog: DialogAuthView,
	) : PacketListener {
    private val transferred: MutableSet<UUID> = ConcurrentHashMap.newKeySet()

    @Subscribe
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        if (plugin.isCoreAccessRevoked()) {
            return
        }
        val username = event.username
        if (!shouldResolve(username)) {
            return
        }
        val resolved = try {
            client.resolvePlayer(username)
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            logger.warn("Failed to resolve Authman player {}", username, ex)
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
        if (plugin.isCoreAccessRevoked()) {
            event.setResult(ResultedEvent.ComponentResult.denied(messages.temporaryUnavailable()))
            return
        }
        val session = sessions.get(event.player.uniqueId) ?: return
        if (session.resolved.locked || session.state == PlayerAuthState.LOCKED) {
            event.setResult(ResultedEvent.ComponentResult.denied(messages.locked()))
            logger.info("Denied locked Authman account {}", event.player.username)
        }
    }

    @Subscribe
    fun onServerPreConnect(event: ServerPreConnectEvent) {
        val player = event.player
        if (plugin.isCoreAccessRevoked()) {
            event.setResult(ServerPreConnectEvent.ServerResult.denied())
            player.disconnect(messages.temporaryUnavailable())
            return
        }
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
        prompt(player, session, force = false)
        logger.info("Held unauthenticated Authman player {} on {} while target is {}", player.username, current.serverInfo.name, event.originalServer.serverInfo.name)
    }

    @Subscribe
    fun onServerConnected(event: ServerConnectedEvent) {
        val player = event.player
        if (plugin.isCoreAccessRevoked()) {
            player.disconnect(messages.temporaryUnavailable())
            return
        }
        val resolved = sessions.resolved(player.uniqueId) ?: resolveConnectedPlayer(player)
        if (resolved != null && !resolved.authRequired && transferred.add(player.uniqueId)) {
            server.scheduler.buildTask(plugin, Runnable {
                if (player.isActive) {
                    transferAfterAuth(player, resolved)
                }
            }).delay(config.completionDelaySeconds, TimeUnit.SECONDS).schedule()
            logger.info("Forwarding non-password Authman player {} to downstream target", player.username)
            return
        }
        val session = sessions.get(player.uniqueId) ?: return
        if (!session.resolved.authRequired || sessions.isAuthenticated(player.uniqueId)) {
            return
        }
        prompt(player, session, force = true)
        scheduleTimeoutCheck(player)
        logger.info("Waiting for Authman password authentication for {}", player.username)
    }

    private fun resolveConnectedPlayer(player: Player): ResolvedPlayer? {
        val resolved = try {
            client.resolvePlayer(player.username)
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            logger.warn("Failed to resolve connected Authman player {}", player.username, ex)
            return null
        }
        sessions.rememberProfile(player.uniqueId, resolved)
        return resolved
    }

    @Subscribe
    fun onPlayerChat(event: PlayerChatEvent) {
        val player = event.player
        val session = sessions.get(player.uniqueId) ?: return
        if (!session.resolved.authRequired || sessions.isAuthenticated(player.uniqueId)) {
            return
        }
        event.setResult(PlayerChatEvent.ChatResult.denied())
        if (!config.dialogFallbackChatEnabled) {
            prompt(player, session, force = true)
            return
        }
        val now = Instant.now()
        if (!sessions.canAcceptChat(player.uniqueId, now)) {
            messages.sendCooldownIgnored(player)
            return
        }
        handlePassword(player, session, event.message.trim())
    }

    override fun onPacketReceive(event: PacketReceiveEvent) {
        if (event.packetType != PacketType.Play.Client.CUSTOM_CLICK_ACTION) {
            return
        }
		val player = event.getPlayer<Player>() ?: return
        val session = sessions.get(player.uniqueId) ?: return
        val submission = DialogAuthView.readSubmission(WrapperPlayClientCustomClickAction(event)) ?: return
        event.setCancelled(true)
        if (submission.sessionId != session.sessionId) {
            logger.debug("Ignored stale Authman dialog submission for {}", player.username)
            return
        }
        if (submission.action == DialogActionType.CANCEL) {
            sessions.clear(player.uniqueId)
            player.disconnect(messages.authTimeout())
            return
        }
        handlePassword(player, session, submission.password.trim())
    }

    @Subscribe
    fun onDisconnect(event: DisconnectEvent) {
        transferred.remove(event.player.uniqueId)
        sessions.clear(event.player.uniqueId)
    }

    private fun shouldResolve(username: String): Boolean {
        return config.resolveRawOfflineNames
    }

    private fun prompt(player: Player, session: PlayerAuthSession, force: Boolean) {
        if (!sessions.shouldPrompt(player.uniqueId, Instant.now(), force = force)) {
            return
        }
        if (config.dialogEnabled) {
            try {
                dialog.showPasswordDialog(player, session.sessionId, session.resolved.protocolName)
                return
            } catch (ex: Exception) {
                logger.warn("Failed to show Authman dialog to {}; falling back to chat prompt", player.username, ex)
            }
        }
        if (config.dialogFallbackChatEnabled) {
            messages.sendPasswordPrompt(player, session)
        }
    }

    private fun handlePassword(player: Player, session: PlayerAuthSession, password: String) {
        if (plugin.isCoreAccessRevoked()) {
            player.disconnect(messages.temporaryUnavailable())
            return
        }
        if (password.isEmpty()) {
            session.lastInputMarker = "/empty"
            prompt(player, session, force = true)
            return
        }
        val result = try {
            client.authenticatePlayer(session.resolved.authUsername, password)
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
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
            prompt(player, session, force = true)
            return
        }
        sessions.markAuthenticated(player.uniqueId)
        messages.sendSuccess(player)
        transferAfterAuth(player, session.resolved)
        logger.info("Authenticated Authman offline account {}", player.username)
    }

    private fun transferAfterAuth(player: Player, resolved: ResolvedPlayer) {
        if (plugin.isCoreAccessRevoked()) {
            player.disconnect(messages.temporaryUnavailable())
            return
        }
        val grant = try {
            client.createTransferGrant(
                username = resolved.protocolName,
                serverId = config.portalRequestedServerId.ifEmpty { config.serverId },
                requestedHost = config.portalRequestedHost,
                source = config.portalSourceId,
            )
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            logger.warn("Failed to create Authman transfer grant for {}", player.username, ex)
            messages.sendTemporaryUnavailable(player)
            return
        }
        try {
            player.storeCookie(Key.key(config.transferCookieKey), grant.token.toByteArray(StandardCharsets.UTF_8))
            player.transferToHost(InetSocketAddress.createUnresolved(grant.target.transferHost, grant.target.transferPort))
            logger.info("Transferred Authman player {} to {}:{}", player.username, grant.target.transferHost, grant.target.transferPort)
        } catch (ex: IllegalArgumentException) {
            logger.warn("Player {} cannot use Minecraft transfer/cookie features", player.username, ex)
            player.disconnect(messages.temporaryUnavailable())
        }
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
