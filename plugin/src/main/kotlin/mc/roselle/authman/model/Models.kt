package mc.roselle.authman.model

import com.velocitypowered.api.proxy.server.RegisteredServer
import com.velocitypowered.api.util.GameProfile
import java.time.Instant
import java.util.UUID

data class ResolvedPlayer(
    val uuid: UUID,
    val protocolName: String,
    val authUsername: String,
    val locked: Boolean,
    val authRequired: Boolean,
    val properties: List<GameProfile.Property>,
)

data class AuthResult(
    val authenticated: Boolean,
    val locked: Boolean,
    val statusCode: Int,
)

data class DownstreamTarget(
    val serverId: String,
    val slug: String,
    val displayName: String,
    val host: String,
    val port: Int,
    val transferHost: String,
    val transferPort: Int,
    val motd: String,
    val gateEnabled: Boolean,
    val grantTtlSeconds: Int,
)

data class TransferGrant(
    val token: String,
    val target: DownstreamTarget,
)

data class GateConsumeResult(
    val allowed: Boolean,
    val resolved: ResolvedPlayer,
    val presenceId: String,
)

enum class PlayerAuthState {
    WAITING_PASSWORD,
    WAITING_EMAIL,
    WAITING_EMAIL_CODE,
    AUTHENTICATED,
    LOCKED,
}

data class PlayerAuthSession(
    val sessionId: String,
    val playerId: UUID,
    val resolved: ResolvedPlayer,
    var state: PlayerAuthState,
    var pendingServer: RegisteredServer?,
    var lastPromptAt: Instant,
    var lastChatAt: Instant,
    var wrongPasswordCount: Int = 0,
    var lastInputMarker: String = "",
)
