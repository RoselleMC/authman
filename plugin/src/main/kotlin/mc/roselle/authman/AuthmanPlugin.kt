package mc.roselle.authman

import com.google.inject.Inject
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.ResultedEvent
import com.velocitypowered.api.event.connection.DisconnectEvent
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.event.player.PlayerChatEvent
import com.velocitypowered.api.event.player.ServerConnectedEvent
import com.velocitypowered.api.event.player.ServerPreConnectEvent
import com.velocitypowered.api.event.proxy.ProxyInitializeEvent
import com.velocitypowered.api.plugin.Plugin
import com.velocitypowered.api.proxy.ProxyServer
import com.velocitypowered.api.proxy.server.RegisteredServer
import com.velocitypowered.api.util.GameProfile
import net.kyori.adventure.text.Component
import org.slf4j.Logger
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.nio.file.Files
import java.nio.file.Path
import java.time.Duration
import java.util.Properties
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

@Plugin(
    id = "authman",
    name = "authman",
    version = "0.1.0-dev",
    description = "Authman Velocity integration plugin",
    authors = ["Score2"],
)
class AuthmanPlugin @Inject constructor(
    private val server: ProxyServer,
    private val logger: Logger,
    @com.velocitypowered.api.plugin.annotation.DataDirectory private val dataDirectory: Path,
) {
    private val httpClient: HttpClient = HttpClient.newBuilder()
        .connectTimeout(Duration.ofSeconds(5))
        .build()

    @Volatile
    private var config: AuthmanConfig = AuthmanConfig.default(dataDirectory)

    private val resolvedProfiles: MutableMap<UUID, ResolvedPlayer> = ConcurrentHashMap()
    private val authenticatedPlayers: MutableSet<UUID> = ConcurrentHashMap.newKeySet()
    private val pendingServers: MutableMap<UUID, RegisteredServer> = ConcurrentHashMap()

    @Subscribe
    fun onProxyInitialize(event: ProxyInitializeEvent) {
        config = AuthmanConfig.load(dataDirectory)
        logger.info(
            "Authman plugin enabled for {} with API {}",
            server.version.name,
            config.apiBase,
        )
    }

    @Subscribe
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        val username = event.username
        if (!username.startsWith("#")) {
            return
        }

        val resolved = try {
            resolvePlayer(username)
        } catch (ex: Exception) {
            logger.warn("Failed to resolve Authman offline player {}", username, ex)
            return
        }

        val profile = event.gameProfile
            .withId(resolved.uuid)
            .withName(resolved.protocolName)
            .withProperties(resolved.properties)
        event.setGameProfile(profile)
        resolvedProfiles[resolved.uuid] = resolved
        logger.info("Applied Authman identity for {} as {} / {} locked={}", username, resolved.protocolName, resolved.uuid, resolved.locked)
    }

    @Subscribe
    fun onLogin(event: LoginEvent) {
        val player = event.player
        val resolved = resolvedProfiles[player.uniqueId]
        if (resolved == null || !resolved.protocolName.startsWith("#")) {
            return
        }
        if (resolved.locked) {
            event.setResult(ResultedEvent.ComponentResult.denied(Component.text("This Authman account is locked.")))
            logger.info("Denied locked Authman account {}", player.username)
        }
    }

    @Subscribe
    fun onServerPreConnect(event: ServerPreConnectEvent) {
        val player = event.player
        val resolved = resolvedProfiles[player.uniqueId] ?: return
        if (!resolved.protocolName.startsWith("#")) {
            return
        }
        if (resolved.locked) {
            event.setResult(ServerPreConnectEvent.ServerResult.denied())
            player.disconnect(Component.text("This Authman account is locked."))
            logger.info("Blocked locked Authman account {} before connecting to {}", player.username, event.originalServer.serverInfo.name)
            return
        }
        if (resolved.authRequired && !authenticatedPlayers.contains(player.uniqueId)) {
            pendingServers[player.uniqueId] = event.originalServer
            logger.info("Waiting for Authman password authentication for {}", player.username)
        }
    }

    @Subscribe
    fun onServerConnected(event: ServerConnectedEvent) {
        val player = event.player
        val resolved = resolvedProfiles[player.uniqueId] ?: return
        if (!resolved.protocolName.startsWith("#") || !resolved.authRequired || authenticatedPlayers.contains(player.uniqueId)) {
            return
        }
        pendingServers[player.uniqueId] = event.server
        player.sendMessage(Component.text("Authman login required. Type your offline password in chat."))
        player.sendMessage(Component.text("Your password will not be forwarded to server chat."))
    }

    @Subscribe
    fun onPlayerChat(event: PlayerChatEvent) {
        val player = event.player
        val resolved = resolvedProfiles[player.uniqueId] ?: return
        if (!resolved.protocolName.startsWith("#") || !resolved.authRequired || authenticatedPlayers.contains(player.uniqueId)) {
            return
        }
        event.setResult(PlayerChatEvent.ChatResult.denied())
        val password = event.message.trim()
        if (password.isEmpty()) {
            player.sendMessage(Component.text("Please type your Authman offline password."))
            return
        }
        val result = try {
            authenticatePlayer(resolved.protocolName, password)
        } catch (ex: Exception) {
            logger.warn("Failed to authenticate Authman player {}", player.username, ex)
            player.sendMessage(Component.text("Authman is temporarily unavailable. Try again later."))
            return
        }
        if (result.locked) {
            pendingServers.remove(player.uniqueId)
            player.disconnect(Component.text("This Authman account is locked."))
            logger.info("Disconnected locked Authman account {} during password authentication", player.username)
            return
        }
        if (!result.authenticated) {
            player.sendMessage(Component.text("Invalid Authman password. Try again."))
            return
        }
        authenticatedPlayers.add(player.uniqueId)
        player.sendMessage(Component.text("Authman login successful. Connecting..."))
        pendingServers.remove(player.uniqueId)
        logger.info("Authenticated Authman offline account {}", player.username)
    }

    @Subscribe
    fun onDisconnect(event: DisconnectEvent) {
        val uuid = event.player.uniqueId
        resolvedProfiles.remove(uuid)
        authenticatedPlayers.remove(uuid)
        pendingServers.remove(uuid)
    }

    private fun authenticatePlayer(username: String, password: String): AuthResult {
        val body = """{"username":${jsonString(username)},"password":${jsonString(password)}}"""
        val request = HttpRequest.newBuilder()
            .uri(config.apiBase.resolve("/api/node/players/authenticate"))
            .timeout(Duration.ofSeconds(8))
            .header("Authorization", "Bearer ${config.nodeToken}")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
            .build()
        val response = httpClient.send(request, HttpResponse.BodyHandlers.ofString())
        if (response.statusCode() in 200..299) {
            return AuthResult(authenticated = true, locked = false)
        }
        if (response.statusCode() == 403 && response.body().contains("auth.account_locked")) {
            return AuthResult(authenticated = false, locked = true)
        }
        if (response.statusCode() == 401) {
            return AuthResult(authenticated = false, locked = false)
        }
        throw IllegalStateException("Authman authenticate failed with HTTP ${response.statusCode()}: ${response.body()}")
    }

    private fun resolvePlayer(username: String): ResolvedPlayer {
        val body = """{"username":${jsonString(username)}}"""
        val request = HttpRequest.newBuilder()
            .uri(config.apiBase.resolve("/api/node/players/resolve"))
            .timeout(Duration.ofSeconds(8))
            .header("Authorization", "Bearer ${config.nodeToken}")
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(body))
            .build()
        val response = httpClient.send(request, HttpResponse.BodyHandlers.ofString())
        if (response.statusCode() !in 200..299) {
            throw IllegalStateException("Authman resolve failed with HTTP ${response.statusCode()}: ${response.body()}")
        }
        return ResolvedPlayer.parse(response.body())
    }
}

data class AuthmanConfig(
    val apiBase: URI,
    val nodeToken: String,
) {
    companion object {
        fun default(dataDirectory: Path): AuthmanConfig {
            return AuthmanConfig(URI.create("http://127.0.0.1:8080"), "")
        }

        fun load(dataDirectory: Path): AuthmanConfig {
            Files.createDirectories(dataDirectory)
            val path = dataDirectory.resolve("authman.properties")
            if (!Files.exists(path)) {
                Files.writeString(
                    path,
                    """
                    apiBase=http://127.0.0.1:8080
                    nodeToken=
                    """.trimIndent() + "\n",
                )
            }
            val properties = Properties()
            Files.newInputStream(path).use { properties.load(it) }
            val apiBase = URI.create(properties.getProperty("apiBase", "http://127.0.0.1:8080").trim())
            val nodeToken = properties.getProperty("nodeToken", "").trim()
            require(nodeToken.isNotEmpty()) { "nodeToken must be configured in $path" }
            return AuthmanConfig(apiBase, nodeToken)
        }
    }
}

data class ResolvedPlayer(
    val uuid: UUID,
    val protocolName: String,
    val locked: Boolean,
    val authRequired: Boolean,
    val properties: List<GameProfile.Property>,
) {
    companion object {
        fun parse(json: String): ResolvedPlayer {
            val uuid = UUID.fromString(requiredString(json, "uuid"))
            val protocolName = requiredString(json, "protocol_name")
            val locked = optionalBoolean(json, "locked") ?: false
            val authRequired = optionalBoolean(json, "required") ?: false
            return ResolvedPlayer(uuid, protocolName, locked, authRequired, emptyList())
        }
    }
}

data class AuthResult(
    val authenticated: Boolean,
    val locked: Boolean,
)

private fun requiredString(json: String, key: String): String {
    val regex = Regex(""""${Regex.escape(key)}"\s*:\s*"([^"]+)"""")
    return regex.find(json)?.groupValues?.get(1)
        ?: throw IllegalArgumentException("missing JSON string field $key")
}

private fun optionalBoolean(json: String, key: String): Boolean? {
    val regex = Regex("\"${Regex.escape(key)}\"\\s*:\\s*(true|false)")
    return regex.find(json)?.groupValues?.get(1)?.toBooleanStrictOrNull()
}

private fun jsonString(value: String): String {
    return buildString {
        append('"')
        for (ch in value) {
            when (ch) {
                '\\' -> append("\\\\")
                '"' -> append("\\\"")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> append(ch)
            }
        }
        append('"')
    }
}
