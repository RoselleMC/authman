package com.iroselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.iroselle.authman.config.RuntimeConfig
import com.iroselle.authman.model.DownstreamConsumeResult
import com.iroselle.authman.model.DownstreamResourcePack
import com.iroselle.authman.model.DownstreamServerOption
import com.iroselle.authman.model.DownstreamTarget
import com.iroselle.authman.model.DownstreamTransferResult
import com.iroselle.authman.model.NodeAction
import com.iroselle.authman.model.NodeActionAck
import com.iroselle.authman.model.NodePresenceCheckRequest
import com.iroselle.authman.model.NodePresenceCheckResult
import com.iroselle.authman.model.PortalLinkResult
import com.iroselle.authman.model.ResolvedPlayer
import com.iroselle.authman.spi.RuntimeTransport
import com.velocitypowered.api.util.GameProfile
import java.util.UUID

class AuthmanClient(
    private val transport: RuntimeTransport,
) {
    private val gson = Gson()

    fun resolvePlayer(username: String): ResolvedPlayer {
        val response = post("/api/node/players/resolve", mapOf("username" to username))
        if (!response.ok) {
            throw AuthmanHttpException("resolve", response)
        }
        val data = response.jsonData()
        val player = data.obj("player")
        val auth = data.obj("auth")
        return ResolvedPlayer(
            uuid = UUID.fromString(player.string("uuid")),
            protocolName = player.string("protocol_name"),
            authUsername = auth.stringOr("username", player.string("protocol_name")),
            locked = player.boolean("locked") || auth.boolean("locked"),
            authRequired = auth.boolean("required"),
            properties = parseProperties(player["properties"]),
        )
    }

    fun consumeTransferGrant(token: String, serverId: String, uuid: String, protocolName: String, source: String, remoteIp: String): DownstreamConsumeResult {
        val response = post(
            "/api/node/downstream/transfer-grants/consume",
            mapOf(
                "token" to token,
                "server_id" to serverId,
                "uuid" to uuid,
                "protocol_name" to protocolName,
                "source" to source,
                "remote_ip" to remoteIp,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("consume transfer grant", response)
        }
        val data = response.jsonData()
        val player = data.obj("player")
        val presence = data.obj("presence")
        val target = data.obj("target")
        val passport = data.obj("passport")
        return DownstreamConsumeResult(
            allowed = true,
            resolved = ResolvedPlayer(
                uuid = UUID.fromString(player.string("uuid")),
                protocolName = player.string("protocol_name"),
                authUsername = player.string("protocol_name"),
                locked = player.boolean("locked"),
                authRequired = false,
                properties = parseProperties(player["properties"]),
            ),
            presenceId = presence.stringOr("id", ""),
            remoteIp = presence.stringOr("remote_addr", data.obj("grant").stringOr("remote_ip", "")),
            target = parseDownstreamTarget(target),
            privilegedPassport = passport.boolean("privileged"),
        )
    }

    fun createDownstreamTransfer(playerId: String, username: String, serverId: String, source: String, remoteIp: String, protocolVersion: Int): DownstreamTransferResult {
        val response = post(
            "/api/node/downstream/transfers",
            mapOf(
                "player_id" to playerId,
                "username" to username,
                "server_id" to serverId,
                "source" to source,
                "remote_ip" to remoteIp,
                "protocol_version" to protocolVersion,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("create downstream transfer", response)
        }
        val data = response.jsonData()
        val player = data.obj("player")
        return DownstreamTransferResult(
            token = data.string("token"),
            resolved = ResolvedPlayer(
                uuid = UUID.fromString(player.string("uuid")),
                protocolName = player.string("protocol_name"),
                authUsername = player.string("protocol_name"),
                locked = player.boolean("locked"),
                authRequired = false,
                properties = parseProperties(player["properties"]),
            ),
            target = parseDownstreamTarget(data.obj("target")),
        )
    }

    fun endPresence(presenceId: String, reason: String) {
        if (presenceId.isBlank()) {
            return
        }
        val response = post(
            "/api/node/presences/end",
            mapOf(
                "presence_id" to presenceId,
                "reason" to reason,
            ),
        )
        if (!response.ok && !response.isAccessRevoked()) {
            throw AuthmanHttpException("end presence", response)
        }
    }

    fun banProfile(username: String, durationSeconds: Long, reason: String) {
        createBan("/api/node/bans/profile", username, durationSeconds, reason)
    }

    fun banPassport(username: String, durationSeconds: Long, reason: String) {
        createBan("/api/node/bans/passport", username, durationSeconds, reason)
    }

    fun renameProfile(profileId: String, protocolName: String, remoteIp: String): String {
        val response = post(
            "/api/node/profiles/rename",
            mapOf(
                "profile_id" to profileId,
                "protocol_name" to protocolName,
                "remote_ip" to remoteIp,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("rename profile", response)
        }
        return response.jsonData().obj("profile").string("protocol_name")
    }

    fun createPortalLink(profileId: String, username: String, serverId: String): PortalLinkResult {
        val response = post(
            "/api/node/portal-links",
            mapOf(
                "profile_id" to profileId,
                "username" to username,
                "server_id" to serverId,
                "ttl" to "10m",
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("create portal link", response)
        }
        val link = response.jsonData().obj("link")
        return PortalLinkResult(
            url = link.string("url"),
            expiresAt = link.string("expires_at"),
        )
    }

    fun ackActions(acks: List<NodeActionAck>) {
        val clean = acks.filter { it.id.isNotBlank() }
        if (clean.isEmpty()) {
            return
        }
        val response = post(
            "/api/node/actions/ack",
            mapOf(
                "ids" to clean.map { it.id.trim() },
                "results" to clean.map { ack ->
                    val row = mutableMapOf<String, Any?>(
                        "id" to ack.id.trim(),
                        "type" to ack.type,
                        "presence_id" to ack.presenceId,
                        "passport_id" to ack.passportId,
                        "profile_id" to ack.profileId,
                        "uuid" to ack.uuid,
                        "protocol_name" to ack.protocolName,
                    )
                    if (ack.online != null) {
                        row["online"] = ack.online
                    }
                    row
                },
            ),
        )
        if (!response.ok && !response.isAccessRevoked()) {
            throw AuthmanHttpException("ack actions", response)
        }
    }

    private fun post(path: String, body: Map<String, Any?>): AuthmanResponse {
        val response = transport.post(path, gson.toJson(body))
        return AuthmanResponse(response.statusCode, response.body, gson)
    }

    private fun createBan(path: String, username: String, durationSeconds: Long, reason: String) {
        val response = post(
            path,
            mapOf(
                "username" to username,
                "expires_in_seconds" to durationSeconds,
                "reason" to reason,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("create ban", response)
        }
    }
}

data class HeartbeatResult(
    val runtime: RuntimeConfig?,
    val actions: List<NodeAction> = emptyList(),
    val downstreamServers: List<DownstreamServerOption> = emptyList(),
    val playerMessages: Map<String, String> = emptyMap(),
)

private val wireGson = Gson()

fun parseRuntimeSync(payload: String): HeartbeatResult =
    parseSync(wireGson.fromJson(payload, JsonObject::class.java))

fun handleRuntimeNodeMessage(
    payload: String,
    onPresenceCheck: (NodePresenceCheckRequest) -> NodePresenceCheckResult,
): String? {
    val root = wireGson.fromJson(payload, JsonObject::class.java)
    if (root.stringOr("type", "") != "presence_check") {
        return null
    }
    return wireGson.toJson(presenceCheckResponse(onPresenceCheck(parsePresenceCheckRequest(root))))
}

private fun parsePlayerMessages(element: JsonElement?): Map<String, String> {
    if (element == null || element.isJsonNull || !element.isJsonObject) {
        return emptyMap()
    }
    val messages = element.asJsonObject["messages"]
    if (messages == null || messages.isJsonNull || !messages.isJsonObject) {
        return emptyMap()
    }
    val out = mutableMapOf<String, String>()
    for ((key, value) in messages.asJsonObject.entrySet()) {
        if (!value.isJsonNull && value.isJsonPrimitive) {
            out[key] = value.asString
        }
    }
    return out
}

private fun parseRuntime(obj: JsonObject): RuntimeConfig {
	return RuntimeConfig(
		nodeName = obj.stringOr("node_name", "velocity"),
		serverId = obj.stringOr("server_id", "default"),
		heartbeatIntervalSeconds = obj.long("heartbeat_interval_seconds", 60),
		transferCookieKey = obj.stringOr("transfer_cookie_key", "authman:transfer_grant"),
		downstreamInitialServer = obj.stringOr("downstream_initial_server", obj.stringOr("gate_initial_server", "")),
		downstreamHoldingServer = obj.stringOr("downstream_holding_server", obj.stringOr("gate_holding_server", "")),
		downstreamValidationTimeoutSeconds = obj.long("downstream_validation_timeout_seconds", obj.long("gate_validation_timeout_seconds", 10)),
        websocketEnabled = obj.boolean("websocket_enabled", true),
        websocketReconnectMinSeconds = obj.long("websocket_reconnect_min_seconds", 2),
        websocketReconnectMaxSeconds = obj.long("websocket_reconnect_max_seconds", 60),
        websocketPingIntervalSeconds = obj.long("websocket_ping_interval_seconds", 25),
	)
}

private fun parseSync(data: JsonObject): HeartbeatResult {
    return HeartbeatResult(
        runtime = parseRuntime(data.obj("runtime_config")),
        actions = parseNodeActions(data["actions"]),
        downstreamServers = parseDownstreamServers(data["downstream_servers"]),
        playerMessages = parsePlayerMessages(data["player_messages"]),
    )
}

private fun parsePresenceCheckRequest(root: JsonObject): NodePresenceCheckRequest {
    val data = root.obj("data")
    val requestId = root.stringOr("request_id", data.stringOr("request_id", ""))
    return NodePresenceCheckRequest(
        requestId = requestId,
        presenceId = data.stringOr("presence_id", ""),
        passportId = data.stringOr("passport_id", ""),
        profileId = data.stringOr("profile_id", ""),
        uuid = data.stringOr("uuid", ""),
        protocolName = data.stringOr("protocol_name", ""),
        reason = data.stringOr("reason", ""),
    )
}

private fun presenceCheckResponse(result: NodePresenceCheckResult): Map<String, Any?> =
    mapOf(
        "type" to "presence_check_result",
        "request_id" to result.requestId,
        "data" to mapOf(
            "request_id" to result.requestId,
            "presence_id" to result.presenceId,
            "passport_id" to result.passportId,
            "profile_id" to result.profileId,
            "uuid" to result.uuid,
            "protocol_name" to result.protocolName,
            "online" to result.online,
        ),
    )

private fun parseNodeActions(element: JsonElement?): List<NodeAction> {
    if (element == null || element.isJsonNull || !element.isJsonArray) {
        return emptyList()
    }
    return element.asJsonArray.mapNotNull { item ->
        val obj = item.asJsonObject ?: return@mapNotNull null
        val id = obj.stringOr("id", "")
        val type = obj.stringOr("type", "")
        if (id.isBlank() || type.isBlank()) {
            return@mapNotNull null
        }
        NodeAction(
            id = id,
            type = type,
            presenceId = obj.stringOr("presence_id", ""),
            passportId = obj.stringOr("passport_id", ""),
            profileId = obj.stringOr("profile_id", ""),
            uuid = obj.stringOr("uuid", ""),
            protocolName = obj.stringOr("protocol_name", ""),
            reason = obj.stringOr("reason", ""),
        )
    }
}

private fun parseDownstreamTarget(obj: JsonObject): DownstreamTarget {
    return DownstreamTarget(
        serverId = obj.stringOr("server_id", ""),
        transferHost = obj.stringOr("transfer_host", ""),
        transferPort = obj.int("transfer_port", 25565),
        resourcePackEnabled = obj.boolean("resource_pack_enabled"),
        resourcePackRequired = obj.boolean("resource_pack_required"),
        resourcePacks = parseResourcePacks(obj["resource_packs"]),
    )
}

private fun parseDownstreamServers(element: JsonElement?): List<DownstreamServerOption> {
    if (element == null || element.isJsonNull || !element.isJsonArray) {
        return emptyList()
    }
    return element.asJsonArray.mapNotNull { item ->
        val obj = item.asJsonObject ?: return@mapNotNull null
        val id = obj.stringOr("id", "").trim()
        if (id.isBlank()) {
            return@mapNotNull null
        }
        DownstreamServerOption(
            id = id,
            slug = obj.stringOr("slug", id),
            displayName = obj.stringOr("display_name", id),
            status = obj.stringOr("status", "active"),
            transferHost = obj.stringOr("transfer_host", ""),
            transferPort = obj.int("transfer_port", 25565),
        )
    }
}

private fun parseResourcePacks(element: JsonElement?): List<DownstreamResourcePack> {
    if (element == null || element.isJsonNull || !element.isJsonArray) {
        return emptyList()
    }
    return element.asJsonArray.mapNotNull { item ->
        val obj = item.asJsonObject ?: return@mapNotNull null
        val url = obj.stringOr("url", "").trim()
        if (url.isBlank()) {
            return@mapNotNull null
        }
        DownstreamResourcePack(
            id = obj.stringOr("id", url).trim().ifBlank { url },
            name = obj.stringOr("name", "").trim(),
            url = url,
            hash = obj.stringOr("hash", "").trim(),
            prompt = obj.stringOr("prompt", "").trim(),
        )
    }
}

data class AuthmanResponse(
    val statusCode: Int,
    val body: String,
    private val gson: Gson,
) {
    val ok: Boolean get() = statusCode in 200..299

    fun isAccessRevoked(): Boolean {
        if (statusCode != 401 && statusCode != 403) {
            return false
        }
        return body.contains("node.revoked") ||
            body.contains("node.unauthorized") ||
            body.contains("node token is invalid") ||
            body.contains("invalid node token")
    }

    fun jsonData(): JsonObject {
        val root = gson.fromJson(body, JsonObject::class.java)
        return root.obj("data")
    }

    fun errorCode(): String = errorField("code")

    fun errorMessage(): String = errorField("message")

    private fun errorField(field: String): String = runCatching {
        gson.fromJson(body, JsonObject::class.java)?.obj("error")?.stringOr(field, "") ?: ""
    }.getOrDefault("")
}

class AuthmanHttpException(operation: String, response: AuthmanResponse) :
    IllegalStateException("Authman $operation failed with HTTP ${response.statusCode}: ${response.body}") {
    val errorCode: String = response.errorCode()
    val errorMessage: String = response.errorMessage()
}

private fun JsonObject.obj(key: String): JsonObject =
    get(key)?.asJsonObject ?: JsonObject()

private fun JsonObject.string(key: String): String =
    get(key)?.takeIf { !it.isJsonNull }?.asString ?: throw IllegalArgumentException("missing JSON string $key")

private fun JsonObject.stringOr(key: String, fallback: String): String =
    get(key)?.takeIf { !it.isJsonNull }?.asString ?: fallback

private fun JsonObject.boolean(key: String, fallback: Boolean = false): Boolean =
    get(key)?.takeIf { !it.isJsonNull }?.asBoolean ?: fallback

private fun JsonObject.int(key: String, fallback: Int = 0): Int =
    get(key)?.takeIf { !it.isJsonNull }?.asInt ?: fallback

private fun JsonObject.long(key: String, fallback: Long = 0): Long =
    get(key)?.takeIf { !it.isJsonNull }?.asLong ?: fallback

private fun parseProperties(element: JsonElement?): List<GameProfile.Property> {
    if (element == null || element.isJsonNull || !element.isJsonArray) {
        return emptyList()
    }
    return element.asJsonArray.mapNotNull { item ->
        val obj = item.asJsonObject ?: return@mapNotNull null
        val name = obj["name"]?.asString ?: return@mapNotNull null
        val value = obj["value"]?.asString ?: return@mapNotNull null
        val signature = obj["signature"]?.takeIf { !it.isJsonNull }?.asString
        GameProfile.Property(name, value, signature ?: "")
    }
}
