package com.iroselle.authman.listener

import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.DisconnectEvent
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.connection.PostLoginEvent
import com.velocitypowered.api.event.player.CookieReceiveEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.event.player.PlayerChooseInitialServerEvent
import com.velocitypowered.api.event.player.PlayerResourcePackStatusEvent
import com.velocitypowered.api.event.player.ServerConnectedEvent
import com.velocitypowered.api.event.player.ServerPreConnectEvent
import com.velocitypowered.api.proxy.Player
import com.velocitypowered.api.proxy.ProxyServer
import com.iroselle.authman.AuthmanPlugin
import com.iroselle.authman.api.AuthmanClient
import com.iroselle.authman.api.AuthmanHttpException
import com.iroselle.authman.config.AuthmanConfig
import com.iroselle.authman.message.AuthmanMessages
import com.iroselle.authman.model.DownstreamResourcePack
import com.iroselle.authman.model.DownstreamTarget
import com.iroselle.authman.model.NodeAction
import com.iroselle.authman.model.NodeActionAck
import com.iroselle.authman.model.NodePresenceCheckRequest
import com.iroselle.authman.model.NodePresenceCheckResult
import net.kyori.adventure.key.Key
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import org.slf4j.Logger
import java.lang.reflect.Field
import java.net.InetAddress
import java.net.InetSocketAddress
import java.nio.charset.StandardCharsets
import java.util.Locale
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
    private val resourcePackTargets: MutableMap<UUID, DownstreamTarget> = ConcurrentHashMap()
    private val privilegedPlayers: MutableSet<UUID> = ConcurrentHashMap.newKeySet()

    fun onlinePresenceCount(): Int =
        server.allPlayers.count { player ->
            player.isActive && allowed.contains(player.uniqueId) && !presences[player.uniqueId].isNullOrBlank()
        }

    fun checkPresenceAction(action: NodeAction): NodeActionAck {
        val player = findPresenceTarget(action)
        val online = player?.isActive == true
        logger.info(
            "Checked Authman presence action {} online={} profile={} presence={} matched={}",
            action.id,
            online,
            action.profileId,
            action.presenceId,
            player?.username ?: "",
        )
        return NodeActionAck(
            id = action.id,
            type = action.type,
            presenceId = action.presenceId,
            passportId = action.passportId,
            profileId = action.profileId,
            uuid = action.uuid,
            protocolName = action.protocolName,
            online = online,
        )
    }

    fun checkPresenceOverWebSocket(request: NodePresenceCheckRequest): NodePresenceCheckResult {
        val action = NodeAction(
            id = request.requestId,
            type = "presence_check",
            presenceId = request.presenceId,
            passportId = request.passportId,
            profileId = request.profileId,
            uuid = request.uuid,
            protocolName = request.protocolName,
            reason = request.reason,
        )
        val player = findPresenceTarget(action)
        val online = player?.isActive == true
        logger.info(
            "Answered Authman websocket presence check {} online={} profile={} presence={} matched={}",
            request.requestId,
            online,
            request.profileId,
            request.presenceId,
            player?.username ?: "",
        )
        return NodePresenceCheckResult(
            requestId = request.requestId,
            presenceId = request.presenceId,
            passportId = request.passportId,
            profileId = request.profileId,
            uuid = request.uuid,
            protocolName = request.protocolName,
            online = online,
        )
    }

    fun disconnectActionTargets(action: NodeAction): Int {
        val component = if (action.reason.isBlank()) {
            messages.defaultDisconnect(action.protocolName)
        } else {
            Component.text(action.reason, NamedTextColor.RED)
        }
        var count = 0
        for (player in server.allPlayers) {
            if (presenceActionMatchesPlayer(action, player)) {
                player.disconnect(component)
                count++
            }
        }
        return count
    }

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
            rejectWith(player, "missing Authman transfer grant cookie", messages.missingTransferGrant(player.username))
            return
        }
        val token = data.toString(StandardCharsets.UTF_8).trim()
        if (token.isEmpty()) {
            validating.remove(player.uniqueId)
            rejectWith(player, "empty Authman transfer grant cookie", messages.missingTransferGrant(player.username))
            return
        }
        val remoteIp = player.remoteAddress.address?.hostAddress ?: player.remoteAddress.hostString
        val result = try {
            client.consumeTransferGrant(
                token = token,
                serverId = config.serverId,
                uuid = player.uniqueId.toString(),
                protocolName = player.username,
                source = remoteIp,
                remoteIp = remoteIp,
            )
        } catch (ex: Exception) {
            plugin.lockIfCoreRejected(ex)
            validating.remove(player.uniqueId)
            if (ex is AuthmanHttpException) {
                when (ex.errorCode) {
                    "auth.banned" -> {
                        rejectWith(player, "banned: ${ex.errorMessage}", messages.banned(ex.errorMessage, player.username))
                        return
                    }
                    "auth.account_locked" -> {
                        rejectWith(player, "account locked", messages.locked(player.username))
                        return
                    }
                    "presence.profile_already_online" -> {
                        rejectWith(player, "profile already online", messages.alreadyOnline(player.username))
                        return
                    }
                }
            }
            reject(player, "invalid Authman transfer grant: ${ex.message}")
            return
        }
        applyLimboRemoteAddress(player, result.remoteIp)
        allowed.add(player.uniqueId)
        if (result.presenceId.isNotBlank()) {
            presences[player.uniqueId] = result.presenceId
        }
        resourcePackTargets[player.uniqueId] = result.target
        if (result.privilegedPassport) {
            privilegedPlayers.add(player.uniqueId)
        } else {
            privilegedPlayers.remove(player.uniqueId)
        }
        validating.remove(player.uniqueId)
        pending.remove(player.uniqueId)
        logger.info("Accepted Authman downstream grant for {} / {}", result.resolved.protocolName, result.resolved.uuid)
        scheduleResourcePacks(player, result.target, 500)
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
        resourcePackTargets.remove(event.player.uniqueId)
        privilegedPlayers.remove(event.player.uniqueId)
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
        val target = resourcePackTargets[event.player.uniqueId] ?: return
        scheduleResourcePacks(event.player, target, 500)
    }

    @Subscribe
    fun onResourcePackStatus(event: PlayerResourcePackStatusEvent) {
        if (!privilegedPlayers.contains(event.player.uniqueId)) {
            return
        }
        if (event.status == PlayerResourcePackStatusEvent.Status.DECLINED ||
            event.status == PlayerResourcePackStatusEvent.Status.FAILED_DOWNLOAD ||
            event.status == PlayerResourcePackStatusEvent.Status.INVALID_URL ||
            event.status == PlayerResourcePackStatusEvent.Status.FAILED_RELOAD
        ) {
            logger.info(
                "Authman privileged passport {} rejected or failed optional resource pack {} with {}",
                event.player.username,
                event.packId,
                event.status,
            )
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

    private fun scheduleResourcePacks(player: Player, target: DownstreamTarget, delayMillis: Long) {
        if (!target.resourcePackEnabled || target.resourcePacks.isEmpty()) {
            return
        }
        server.scheduler.buildTask(plugin, Runnable {
            sendResourcePacks(player, target)
        }).delay(delayMillis.coerceAtLeast(0), TimeUnit.MILLISECONDS).schedule()
    }

    private fun sendResourcePacks(player: Player, target: DownstreamTarget) {
        if (!player.isActive || !allowed.contains(player.uniqueId)) {
            return
        }
        for (pack in target.resourcePacks) {
            sendResourcePack(player, pack, target.resourcePackRequired)
        }
    }

    private fun sendResourcePack(player: Player, pack: DownstreamResourcePack, required: Boolean) {
        val url = pack.url.trim()
        if (url.isBlank()) {
            return
        }
        try {
            val builder = server.createResourcePackBuilder(url)
                .setId(stableResourcePackId(pack))
                .setShouldForce(required && !privilegedPlayers.contains(player.uniqueId))
            val hash = parseSha1(pack.hash)
            if (hash != null) {
                builder.setHash(hash)
            }
            val prompt = pack.prompt.trim()
            if (prompt.isNotEmpty()) {
                builder.setPrompt(Component.text(prompt))
            }
            player.sendResourcePackOffer(builder.build())
            logger.info("Sent Authman resource pack {} to {}", pack.id, player.username)
        } catch (ex: Exception) {
            logger.warn("Failed to send Authman resource pack {} to {}", pack.id, player.username, ex)
        }
    }

    private fun stableResourcePackId(pack: DownstreamResourcePack): UUID {
        return try {
            UUID.fromString(pack.id)
        } catch (_: IllegalArgumentException) {
            UUID.nameUUIDFromBytes("authman:resource-pack:${pack.id}:${pack.url}".toByteArray(StandardCharsets.UTF_8))
        }
    }

    private fun parseSha1(value: String): ByteArray? {
        val clean = value.trim().lowercase(Locale.ROOT)
        if (clean.isEmpty()) {
            return null
        }
        if (clean.length != 40 || clean.any { it !in '0'..'9' && it !in 'a'..'f' }) {
            logger.warn("Ignoring invalid Authman resource pack SHA-1: {}", value)
            return null
        }
        return ByteArray(20) { index ->
            clean.substring(index * 2, index * 2 + 2).toInt(16).toByte()
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
            rejectWith(player, "client does not support transfer cookies", messages.transferUnsupported(player.username))
        }
    }

    private fun cookieKey(): Key = Key.key(config.transferCookieKey)

    private fun scheduleValidationTimeout(player: Player) {
        server.scheduler.buildTask(plugin, Runnable {
            if (player.isActive && pending.contains(player.uniqueId) && !allowed.contains(player.uniqueId)) {
                rejectWith(player, "Authman downstream validation timed out", messages.validationTimeout(player.username))
            }
        }).delay(config.downstreamValidationTimeoutSeconds, TimeUnit.SECONDS).schedule()
    }

    private fun reject(player: Player, reason: String) {
        rejectWith(player, reason, messages.temporaryUnavailable())
    }

    private fun rejectWith(player: Player, reason: String, message: Component) {
        pending.remove(player.uniqueId)
        allowed.remove(player.uniqueId)
        validating.remove(player.uniqueId)
        logger.info("Rejected Authman downstream player {}: {}", player.username, reason)
        if (player.isActive) {
            player.disconnect(message)
        }
    }

    private fun applyLimboRemoteAddress(player: Player, remoteIp: String) {
        val cleanIp = remoteIp.trim()
        if (cleanIp.isBlank()) {
            logger.info("Authman consume response did not include a Limbo remote IP for {}; keeping Velocity remote address {}", player.username, player.remoteAddress)
            return
        }
        val current = player.remoteAddress
        val target = try {
            InetSocketAddress(InetAddress.getByName(cleanIp), current.port)
        } catch (ex: Exception) {
            logger.warn("Ignoring invalid Authman Limbo remote IP {} for {}", cleanIp, player.username, ex)
            return
        }
        val currentHost = current.address?.hostAddress ?: current.hostString
        val targetHost = target.address?.hostAddress ?: target.hostString
        if (currentHost == targetHost) {
            return
        }
        val changed = runCatching {
            val connectionMethod = player.javaClass.methods.firstOrNull { method ->
                method.name == "getConnection" && method.parameterCount == 0
            } ?: return@runCatching false
            connectionMethod.isAccessible = true
            val connection = connectionMethod.invoke(player) ?: return@runCatching false
            val remoteAddressField = findField(connection.javaClass, "remoteAddress") ?: return@runCatching false
            remoteAddressField.isAccessible = true
            remoteAddressField.set(connection, target)
            true
        }.getOrElse { ex ->
            logger.warn("Failed to apply Authman Limbo remote IP {} for {}", cleanIp, player.username, ex)
            false
        }
        if (changed) {
            logger.info("Applied Authman Limbo remote IP for {}: {} -> {}", player.username, currentHost, targetHost)
        } else {
            logger.warn("Velocity player implementation does not expose a writable remote address for {}; keeping {}", player.username, currentHost)
        }
    }

    private fun findField(type: Class<*>, name: String): Field? {
        var current: Class<*>? = type
        while (current != null) {
            try {
                return current.getDeclaredField(name)
            } catch (_: NoSuchFieldException) {
                current = current.superclass
            }
        }
        return null
    }

    private fun findPresenceTarget(action: NodeAction): Player? =
        server.allPlayers.firstOrNull { player -> presenceActionMatchesPlayer(action, player) }

    private fun presenceActionMatchesPlayer(action: NodeAction, player: Player): Boolean {
        if (!player.isActive || !allowed.contains(player.uniqueId)) {
            return false
        }
        val requestedPresenceID = action.presenceId.trim()
        if (requestedPresenceID.isNotEmpty() && presences[player.uniqueId] != requestedPresenceID) {
            return false
        }
        return identityActionMatchesPlayer(action, player)
    }

    private fun identityActionMatchesPlayer(action: NodeAction, player: Player): Boolean {
        val uuid = runCatching {
            action.uuid.takeIf { it.isNotBlank() }?.let(UUID::fromString)
        }.getOrNull()
        if (uuid != null) {
            return player.uniqueId == uuid
        }
        return action.protocolName.isNotBlank() && player.username.equals(action.protocolName, ignoreCase = true)
    }
}
