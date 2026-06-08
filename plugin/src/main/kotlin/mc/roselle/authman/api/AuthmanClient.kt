package mc.roselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.velocitypowered.api.util.GameProfile
import mc.roselle.authman.config.AuthmanConfig
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
        val display = data.obj("display")
        return ResolvedPlayer(
            uuid = UUID.fromString(player.string("uuid")),
            protocolName = player.string("protocol_name"),
            locked = player.boolean("locked") || auth.boolean("locked"),
            authRequired = auth.boolean("required"),
            properties = parseProperties(player["properties"]),
            stripOfflinePrefix = display.boolean("strip_offline_prefix", fallback = true),
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
        return GateConsumeResult(
            allowed = true,
            resolved = ResolvedPlayer(
                uuid = UUID.fromString(player.string("uuid")),
                protocolName = player.string("protocol_name"),
                locked = player.boolean("locked"),
                authRequired = false,
                properties = parseProperties(player["properties"]),
                stripOfflinePrefix = false,
            ),
        )
    }

    fun heartbeat(
        nodeName: String,
        serverId: String,
        pluginVersion: String,
        velocityVersion: String,
    ): AuthmanResponse {
        return post(
            "/api/node/heartbeat",
            mapOf(
                "name" to nodeName,
                "server_id" to serverId,
                "instance_fingerprint" to instanceFingerprint,
                "plugin_version" to pluginVersion,
                "velocity_version" to velocityVersion,
            ),
            includeInstanceHeader = false,
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
}

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

data class AuthmanResponse(
    val statusCode: Int,
    val body: String,
    private val gson: Gson,
) {
    val ok: Boolean get() = statusCode in 200..299

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

private fun JsonObject.boolean(key: String, fallback: Boolean = false): Boolean =
    get(key)?.takeIf { !it.isJsonNull }?.asBoolean ?: fallback

private fun JsonObject.int(key: String, fallback: Int = 0): Int =
    get(key)?.takeIf { !it.isJsonNull }?.asInt ?: fallback

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
