package mc.roselle.authman.model

import com.velocitypowered.api.util.GameProfile
import java.util.UUID

data class ResolvedPlayer(
    val uuid: UUID,
    val protocolName: String,
    val authUsername: String,
    val locked: Boolean,
    val authRequired: Boolean,
    val properties: List<GameProfile.Property>,
)

data class DownstreamConsumeResult(
    val allowed: Boolean,
    val resolved: ResolvedPlayer,
    val presenceId: String,
)

data class NodeAction(
    val id: String,
    val type: String,
    val presenceId: String,
    val passportId: String,
    val profileId: String,
    val uuid: String,
    val protocolName: String,
    val reason: String,
)
