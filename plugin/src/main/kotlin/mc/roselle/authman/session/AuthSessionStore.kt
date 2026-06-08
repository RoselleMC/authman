package mc.roselle.authman.session

import com.velocitypowered.api.proxy.server.RegisteredServer
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.model.PlayerAuthSession
import mc.roselle.authman.model.PlayerAuthState
import mc.roselle.authman.model.ResolvedPlayer
import java.time.Duration
import java.time.Instant
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

class AuthSessionStore(private val config: AuthmanConfig) {
    private val sessions: MutableMap<UUID, PlayerAuthSession> = ConcurrentHashMap()
    private val offlineProfilesByProtocolName: MutableMap<String, ResolvedPlayer> = ConcurrentHashMap()
    private val offlineProfilesByUUID: MutableMap<UUID, ResolvedPlayer> = ConcurrentHashMap()

    val authenticatedPlayers: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    val pendingServers: MutableMap<UUID, RegisteredServer> = ConcurrentHashMap()

    fun rememberProfile(playerId: UUID, resolved: ResolvedPlayer) {
        offlineProfilesByProtocolName[resolved.protocolName] = resolved
        offlineProfilesByUUID[resolved.uuid] = resolved
        if (!resolved.authRequired) {
            return
        }
        val state = if (resolved.locked) PlayerAuthState.LOCKED else PlayerAuthState.WAITING_PASSWORD
        sessions[playerId] = PlayerAuthSession(
            sessionId = UUID.randomUUID().toString(),
            playerId = playerId,
            resolved = resolved,
            state = state,
            pendingServer = pendingServers[playerId],
            lastPromptAt = Instant.EPOCH,
            lastChatAt = Instant.EPOCH,
        )
        if (state == PlayerAuthState.LOCKED) {
            authenticatedPlayers.remove(playerId)
        }
    }

    fun get(playerId: UUID): PlayerAuthSession? = sessions[playerId]

    fun resolved(playerId: UUID): ResolvedPlayer? = offlineProfilesByUUID[playerId]

    fun markPending(playerId: UUID, server: RegisteredServer) {
        pendingServers[playerId] = server
        sessions[playerId]?.pendingServer = server
    }

    fun markAuthenticated(playerId: UUID) {
        authenticatedPlayers.add(playerId)
        pendingServers.remove(playerId)
        sessions[playerId]?.let {
            it.state = PlayerAuthState.AUTHENTICATED
            it.pendingServer = null
            it.wrongPasswordCount = 0
            it.lastInputMarker = ""
        }
    }

    fun markWrongPassword(playerId: UUID): Int {
        val session = sessions[playerId] ?: return 0
        session.wrongPasswordCount += 1
        session.lastInputMarker = "/wrongpassword"
        return session.wrongPasswordCount
    }

    fun markLocked(playerId: UUID) {
        authenticatedPlayers.remove(playerId)
        pendingServers.remove(playerId)
        sessions[playerId]?.state = PlayerAuthState.LOCKED
    }

    fun isAuthenticated(playerId: UUID): Boolean = authenticatedPlayers.contains(playerId)

    fun canAcceptChat(playerId: UUID, now: Instant): Boolean {
        val session = sessions[playerId] ?: return true
        if (Duration.between(session.lastChatAt, now).toMillis() < config.chatCooldownMillis) {
            return false
        }
        session.lastChatAt = now
        return true
    }

    fun shouldPrompt(playerId: UUID, now: Instant, force: Boolean = false): Boolean {
        val session = sessions[playerId] ?: return false
        if (force || Duration.between(session.lastPromptAt, now).seconds >= 6) {
            session.lastPromptAt = now
            return true
        }
        return false
    }

    fun expiredWaitingSessions(now: Instant): List<PlayerAuthSession> {
        return sessions.values.filter {
            it.state == PlayerAuthState.WAITING_PASSWORD &&
                Duration.between(it.lastPromptAt, now).seconds > config.authTimeoutSeconds
        }
    }

    fun outgoingNameFor(protocolName: String): String? {
        val resolved = offlineProfilesByProtocolName[protocolName] ?: return null
        if (!shouldStripOfflinePrefix(resolved)) {
            return null
        }
        return resolved.publicName
    }

    fun outgoingNameFor(playerId: UUID, protocolName: String): String? {
        val resolved = offlineProfilesByUUID[playerId] ?: offlineProfilesByProtocolName[protocolName] ?: return null
        if (!shouldStripOfflinePrefix(resolved)) {
            return null
        }
        return resolved.publicName
    }

    fun clear(playerId: UUID) {
        val session = sessions.remove(playerId)
        val resolved = session?.resolved ?: offlineProfilesByUUID[playerId]
        if (resolved != null) {
            offlineProfilesByProtocolName.remove(resolved.protocolName)
            offlineProfilesByUUID.remove(resolved.uuid)
        }
        authenticatedPlayers.remove(playerId)
        pendingServers.remove(playerId)
    }

    private fun shouldStripOfflinePrefix(resolved: ResolvedPlayer): Boolean {
        val strip = config.stripOfflinePrefix
        if (!strip.enabled) {
            return false
        }
        return resolved.stripOfflinePrefix || strip.stripWhenPremiumNameExists
    }
}
