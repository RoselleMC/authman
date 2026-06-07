package mc.roselle.authman.packet

import com.github.retrooper.packetevents.event.PacketListener
import com.github.retrooper.packetevents.event.PacketSendEvent
import com.github.retrooper.packetevents.protocol.packettype.PacketType
import com.github.retrooper.packetevents.wrapper.play.server.WrapperPlayServerPlayerInfo
import com.github.retrooper.packetevents.wrapper.play.server.WrapperPlayServerPlayerInfoUpdate
import com.github.retrooper.packetevents.wrapper.play.server.WrapperPlayServerTeams
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.session.AuthSessionStore
import org.slf4j.Logger

class OfflinePrefixPacketListener(
    private val config: AuthmanConfig,
    private val sessions: AuthSessionStore,
    private val logger: Logger,
) : PacketListener {
    override fun onPacketSend(event: PacketSendEvent) {
        if (!config.stripOfflinePrefix.enabled) {
            return
        }
        try {
            when (event.packetType) {
                PacketType.Play.Server.PLAYER_INFO_UPDATE -> rewritePlayerInfoUpdate(event)
                PacketType.Play.Server.PLAYER_INFO -> rewriteLegacyPlayerInfo(event)
                PacketType.Play.Server.TEAMS -> rewriteTeams(event)
            }
        } catch (ex: Exception) {
            logger.warn("Failed to rewrite Authman offline prefix in outgoing packet {}", event.packetType, ex)
        }
    }

    private fun rewritePlayerInfoUpdate(event: PacketSendEvent) {
        if (!config.stripOfflinePrefix.playerInfoPackets) {
            return
        }
        val packet = WrapperPlayServerPlayerInfoUpdate(event)
        var changed = false
        for (entry in packet.entries) {
            val profile = entry.gameProfile ?: continue
            val currentName = profile.name ?: continue
            val replacement = sessions.outgoingNameFor(profile.uuid, currentName) ?: continue
            profile.name = replacement
            changed = true
        }
        if (changed) {
            event.markForReEncode(true)
        }
    }

    private fun rewriteLegacyPlayerInfo(event: PacketSendEvent) {
        if (!config.stripOfflinePrefix.playerInfoPackets) {
            return
        }
        val packet = WrapperPlayServerPlayerInfo(event)
        var changed = false
        for (entry in packet.playerDataList) {
            val profile = entry.userProfile ?: continue
            val currentName = profile.name ?: continue
            val replacement = sessions.outgoingNameFor(profile.uuid, currentName) ?: continue
            profile.name = replacement
            changed = true
        }
        if (changed) {
            event.markForReEncode(true)
        }
    }

    private fun rewriteTeams(event: PacketSendEvent) {
        if (!config.stripOfflinePrefix.scoreboardTeamPackets) {
            return
        }
        val packet = WrapperPlayServerTeams(event)
        val rewritten = packet.players.map { sessions.outgoingNameFor(it) ?: it }
        if (rewritten != packet.players.toList()) {
            packet.players = rewritten
            event.markForReEncode(true)
        }
    }
}
