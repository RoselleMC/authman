package mc.roselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.velocitypowered.api.util.GameProfile
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.config.RuntimeConfig
import mc.roselle.authman.model.AuthResult
import mc.roselle.authman.model.DownstreamTarget
import mc.roselle.authman.model.GateConsumeResult
import mc.roselle.authman.model.ResolvedPlayer
import mc.roselle.authman.model.TransferGrant
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

    fun resolvePortalTarget(serverId: String, requestedHost: String): DownstreamTarget {
        val response = post(
            "/api/node/portal/targets/resolve",
            mapOf(
                "server_id" to serverId,
                "requested_host" to requestedHost,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("resolve portal target", response)
        }
        return parseTarget(response.jsonData().obj("target"))
    }

    fun authenticatePlayer(username: String, password: String): AuthResult {
        val response = post("/api/node/players/authenticate", mapOf("username" to username, "password" to password))
        if (response.ok) {
            return AuthResult(authenticated = true, locked = false, statusCode = response.statusCode)
        }
        if (response.statusCode == 403 && response.body.contains("auth.account_locked")) {
            return AuthResult(authenticated = false, locked = true, statusCode = response.statusCode)
        }
        if (response.statusCode == 401) {
            return AuthResult(authenticated = false, locked = false, statusCode = response.statusCode)
        }
        throw AuthmanHttpException("authenticate", response)
    }

    fun createTransferGrant(username: String, serverId: String, requestedHost: String, source: String): TransferGrant {
        val response = post(
            "/api/node/portal/transfer-grants",
            mapOf(
                "username" to username,
                "server_id" to serverId,
                "requested_host" to requestedHost,
                "source" to source,
            ),
        )
        if (!response.ok) {
            throw AuthmanHttpException("create transfer grant", response)
        }
        val data = response.jsonData()
        return TransferGrant(
            token = data.string("token"),
            target = parseTarget(data.obj("target")),
        )
    }

    fun consumeTransferGrant(token: String, serverId: String, uuid: String, protocolName: String, source: String): GateConsumeResult {
        val response = post(
            "/api/node/gate/transfer-grants/consume",
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
        return GateConsumeResult(
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
                "mode" to config.runtimeMode.name.lowercase(),
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
            accessRevoked = false,
        )
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
    val accessRevoked: Boolean,
)

private fun parseTarget(target: JsonObject): DownstreamTarget {
    return DownstreamTarget(
        serverId = target.string("server_id"),
        slug = target.string("slug"),
        displayName = target.string("display_name"),
        host = target.string("host"),
        port = target.int("port", 25565),
        transferHost = target.string("transfer_host"),
        transferPort = target.int("transfer_port", 25565),
        motd = target.string("motd"),
        gateEnabled = target.boolean("gate_enabled", true),
        grantTtlSeconds = target.int("grant_ttl_seconds", 45),
    )
}

private fun parseRuntime(obj: JsonObject): RuntimeConfig {
    return RuntimeConfig(
        nodeName = obj.stringOr("node_name", "velocity"),
        serverId = obj.stringOr("server_id", "default"),
        heartbeatIntervalSeconds = obj.long("heartbeat_interval_seconds", 60),
        resolveRawOfflineNames = obj.boolean("resolve_raw_offline_names", true),
        maxPasswordAttempts = obj.int("max_password_attempts", 3),
        chatCooldownMillis = obj.long("chat_cooldown_millis", 150),
        authTimeoutSeconds = obj.long("auth_timeout_seconds", 90),
        completionDelaySeconds = obj.long("completion_delay_seconds", 3),
        defaultTargetServer = obj.stringOr("default_target_server", ""),
        holdingServer = obj.stringOr("holding_server", ""),
        transferCookieKey = obj.stringOr("transfer_cookie_key", "authman:transfer_grant"),
        gateInitialServer = obj.stringOr("gate_initial_server", ""),
        gateHoldingServer = obj.stringOr("gate_holding_server", ""),
        gateValidationTimeoutSeconds = obj.long("gate_validation_timeout_seconds", 10),
        portalRequestedServerId = obj.stringOr("portal_requested_server_id", ""),
        portalRequestedHost = obj.stringOr("portal_requested_host", ""),
        portalSourceId = obj.stringOr("portal_source_id", ""),
        dialogEnabled = obj.boolean("dialog_enabled", true),
        dialogFallbackChatEnabled = obj.boolean("dialog_fallback_chat_enabled", true),
        emailVerificationMode = obj.stringOr("email_verification_mode", "disabled"),
    )
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
