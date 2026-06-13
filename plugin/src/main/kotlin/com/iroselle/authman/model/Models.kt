package com.iroselle.authman.model

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
    val target: DownstreamTarget,
    val privilegedPassport: Boolean,
)

data class DownstreamTarget(
    val serverId: String,
    val transferHost: String,
    val transferPort: Int,
    val resourcePackEnabled: Boolean,
    val resourcePackRequired: Boolean,
    val resourcePacks: List<DownstreamResourcePack>,
)

data class DownstreamResourcePack(
    val id: String,
    val name: String,
    val url: String,
    val hash: String,
    val prompt: String,
)

data class DownstreamServerOption(
	val id: String,
	val slug: String,
	val displayName: String,
	val status: String,
	val transferHost: String,
	val transferPort: Int,
)

data class DownstreamStatusReport(
	val onlinePlayers: Int,
	val maxPlayers: Int,
)

data class DownstreamTransferResult(
	val token: String,
	val resolved: ResolvedPlayer,
    val target: DownstreamTarget,
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

data class NodeActionAck(
    val id: String,
    val type: String = "",
    val presenceId: String = "",
    val passportId: String = "",
    val profileId: String = "",
    val uuid: String = "",
    val protocolName: String = "",
    val online: Boolean? = null,
)

data class NodePresenceCheckRequest(
    val requestId: String,
    val presenceId: String,
    val passportId: String,
    val profileId: String,
    val uuid: String,
    val protocolName: String,
    val reason: String,
)

data class NodePresenceCheckResult(
    val requestId: String,
    val presenceId: String,
    val passportId: String,
    val profileId: String,
    val uuid: String,
    val protocolName: String,
    val online: Boolean,
)
