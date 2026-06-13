package com.iroselle.authman.api

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.velocitypowered.api.util.GameProfile
import com.iroselle.authman.config.AuthmanConfig
import com.iroselle.authman.config.RuntimeConfig
import com.iroselle.authman.model.DownstreamConsumeResult
import com.iroselle.authman.model.DownstreamResourcePack
import com.iroselle.authman.model.DownstreamServerOption
import com.iroselle.authman.model.DownstreamTarget
import com.iroselle.authman.model.DownstreamTransferResult
import com.iroselle.authman.model.NodeAction
import com.iroselle.authman.model.ResolvedPlayer
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.net.http.WebSocket
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CompletionStage
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
            downstreamServers = parseDownstreamServers(data["downstream_servers"]),
            playerMessages = parsePlayerMessages(data["player_messages"]),
            accessRevoked = false,
        )
    }

    fun connectEvents(onSync: (HeartbeatResult) -> Unit, onRevoked: () -> Unit): CompletableFuture<Void> {
        val done = CompletableFuture<Void>()
        val buffer = StringBuilder()
        httpClient.newWebSocketBuilder()
            .header("Authorization", "Bearer ${config.nodeToken}")
            .header("X-Authman-Instance", instanceFingerprint)
            .buildAsync(resolveCoreWebSocketPath("/api/node/events"), object : WebSocket.Listener {
                override fun onText(webSocket: WebSocket, data: CharSequence, last: Boolean): CompletionStage<*> {
                    buffer.append(data)
                    if (last) {
                        val message = buffer.toString()
                        buffer.setLength(0)
                        try {
                            val root = gson.fromJson(message, JsonObject::class.java)
                            when (root.stringOr("type", "")) {
                                "sync" -> onSync(parseSync(root.obj("data")))
                                "revoked" -> {
                                    onRevoked()
                                    done.completeExceptionally(IllegalStateException("Authman node token was revoked"))
                                }
                                "error" -> done.completeExceptionally(IllegalStateException(root.obj("error").stringOr("message", "Authman node event stream failed")))
                            }
                        } catch (ex: Exception) {
                            done.completeExceptionally(ex)
                        }
                    }
                    webSocket.request(1)
                    return CompletableFuture.completedFuture(null)
                }

                override fun onError(webSocket: WebSocket, error: Throwable) {
                    done.completeExceptionally(error)
                }

                override fun onClose(webSocket: WebSocket, statusCode: Int, reason: String): CompletionStage<*> {
                    done.complete(null)
                    return CompletableFuture.completedFuture(null)
                }
            })
            .whenComplete { webSocket, error ->
                if (error != null) {
                    done.completeExceptionally(error)
                } else {
                    webSocket.request(1)
                }
            }
        return done
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
            .uri(resolveCorePath(path))
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

    private fun resolveCorePath(path: String) =
        java.net.URI.create("${config.apiBase.toString().trimEnd('/')}/${path.trimStart('/')}")

    private fun resolveCoreWebSocketPath(path: String): java.net.URI {
        val base = config.apiBase.toString().trimEnd('/')
        val uri = java.net.URI.create("$base/${path.trimStart('/')}")
        val scheme = when (uri.scheme.lowercase()) {
            "https" -> "wss"
            "http" -> "ws"
            "wss", "ws" -> uri.scheme
            else -> "ws"
        }
        return java.net.URI(scheme, uri.userInfo, uri.host, uri.port, uri.path, uri.query, uri.fragment)
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
    val downstreamServers: List<DownstreamServerOption> = emptyList(),
    val playerMessages: Map<String, String> = emptyMap(),
    val accessRevoked: Boolean,
)

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
        ok = true,
        statusCode = 200,
        body = "",
        runtime = parseRuntime(data.obj("runtime_config")),
        actions = parseNodeActions(data["actions"]),
        downstreamServers = parseDownstreamServers(data["downstream_servers"]),
        playerMessages = parsePlayerMessages(data["player_messages"]),
        accessRevoked = false,
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
