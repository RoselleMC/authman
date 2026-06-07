package mc.roselle.authman.model

import com.velocitypowered.api.proxy.server.RegisteredServer
import com.velocitypowered.api.util.GameProfile
import java.time.Instant
import java.util.UUID

data class ResolvedPlayer(
    val uuid: UUID,
    val protocolName: String,
    val locked: Boolean,
    val authRequired: Boolean,
    val properties: List<GameProfile.Property>,
    val stripOfflinePrefix: Boolean,
) {
    val offline: Boolean get() = protocolName.startsWith("#")
    val publicName: String get() = protocolName.removePrefix("#")
}

data class AuthResult(
    val authenticated: Boolean,
    val locked: Boolean,
    val statusCode: Int,
)

enum class PlayerAuthState {
    WAITING_PASSWORD,
    WAITING_EMAIL,
    WAITING_EMAIL_CODE,
    AUTHENTICATED,
    LOCKED,
}

data class PlayerAuthSession(
    val playerId: UUID,
    val resolved: ResolvedPlayer,
    var state: PlayerAuthState,
    var pendingServer: RegisteredServer?,
    var lastPromptAt: Instant,
    var lastChatAt: Instant,
    var wrongPasswordCount: Int = 0,
    var lastInputMarker: String = "",
)
