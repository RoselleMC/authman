package mc.roselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.velocitypowered.api.util.GameProfile
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.config.RuntimeConfig
import mc.roselle.authman.model.DownstreamConsumeResult
import mc.roselle.authman.model.NodeAction
import mc.roselle.authman.model.ResolvedPlayer
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.util.UUID

class AuthmanClient(
    private val config: AuthmanConfig,
    private val instanceFingerprint: String,
) {
    private val gson = Gson()
    private val httpClient: HttpClient = HttpClient.newBuilder()
        .connectTimeout(config.requestTimeout)
        .build()

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

    fun consumeTransferGrant(token: String, serverId: String, uuid: String, protocolName: String, source: String): DownstreamConsumeResult {
        val response = post(
            "/api/node/downstream/transfer-grants/consume",
            mapOf(
                "token" to token,
                "server_id" to serverId,
                "uuid" to uuid,
                "protocol_name" to protocolName,
                "source" to source,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("consume transfer grant", response)
        }
        val player = response.jsonData().obj("player")
        val presence = response.jsonData().obj("presence")
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

    fun heartbeat(pluginVersion: String, velocityVersion: String): HeartbeatResult {
        val response = post(
            "/api/node/heartbeat",
            mapOf(
                "kind" to "downstream_velocity",
                "instance_fingerprint" to instanceFingerprint,
                "plugin_version" to pluginVersion,
                "velocity_version" to velocityVersion,
            ),
            includeInstanceHeader = false,
        )
        if (!response.ok) {
            return HeartbeatResult(
                ok = false,
                statusCode = response.statusCode,
                body = response.body,
                runtime = null,
                accessRevoked = response.isAccessRevoked(),
            )
        }
        val data = response.jsonData()
        return HeartbeatResult(
            ok = true,
            statusCode = response.statusCode,
            body = response.body,
            runtime = parseRuntime(data.obj("runtime_config")),
            actions = parseNodeActions(data["actions"]),
            accessRevoked = false,
        )
    }

    fun ackActions(ids: List<String>) {
        val clean = ids.map { it.trim() }.filter { it.isNotEmpty() }
        if (clean.isEmpty()) {
            return
        }
        val response = post("/api/node/actions/ack", mapOf("ids" to clean))
        if (!response.ok && !response.isAccessRevoked()) {
            throw AuthmanHttpException("ack actions", response)
        }
    }

    private fun post(path: String, body: Map<String, Any?>, includeInstanceHeader: Boolean = true): AuthmanResponse {
        val builder = HttpRequest.newBuilder()
            .uri(config.apiBase.resolve(path))
            .timeout(config.requestTimeout)
            .header("Authorization", "Bearer ${config.nodeToken}")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(gson.toJson(body)))
        if (includeInstanceHeader) {
            builder.header("X-Authman-Instance", instanceFingerprint)
        }
        val response = httpClient.send(builder.build(), HttpResponse.BodyHandlers.ofString())
        return AuthmanResponse(response.statusCode(), response.body(), gson)
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
    val ok: Boolean,
    val statusCode: Int,
    val body: String,
    val runtime: RuntimeConfig?,
    val actions: List<NodeAction> = emptyList(),
    val accessRevoked: Boolean,
)

private fun parseRuntime(obj: JsonObject): RuntimeConfig {
	return RuntimeConfig(
		nodeName = obj.stringOr("node_name", "velocity"),
		serverId = obj.stringOr("server_id", "default"),
		heartbeatIntervalSeconds = obj.long("heartbeat_interval_seconds", 60),
		transferCookieKey = obj.stringOr("transfer_cookie_key", "authman:transfer_grant"),
		downstreamInitialServer = obj.stringOr("downstream_initial_server", obj.stringOr("gate_initial_server", "")),
		downstreamHoldingServer = obj.stringOr("downstream_holding_server", obj.stringOr("gate_holding_server", "")),
		downstreamValidationTimeoutSeconds = obj.long("downstream_validation_timeout_seconds", obj.long("gate_validation_timeout_seconds", 10)),
	)
}

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
}

class AuthmanHttpException(operation: String, response: AuthmanResponse) :
    IllegalStateException("Authman $operation failed with HTTP ${response.statusCode}: ${response.body}")

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
