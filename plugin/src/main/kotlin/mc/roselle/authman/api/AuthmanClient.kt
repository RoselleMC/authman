package mc.roselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.velocitypowered.api.util.GameProfile
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.model.AuthResult
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

    private fun post(path: String, body: Map<String, String>, includeInstanceHeader: Boolean = true): AuthmanResponse {
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
