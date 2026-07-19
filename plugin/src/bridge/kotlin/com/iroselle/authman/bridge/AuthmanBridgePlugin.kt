package com.iroselle.authman.bridge

import com.google.gson.Gson
import com.iroselle.authman.bridge.api.AuthmanBridge
import com.iroselle.authman.bridge.api.AuthmanBridgeMessage
import com.iroselle.authman.bridge.api.AuthmanBridgeResult
import com.iroselle.authman.bridge.protocol.BridgeRequest
import com.iroselle.authman.bridge.protocol.BridgeResponse
import com.iroselle.authman.bridge.protocol.BridgeSender
import com.iroselle.authman.bridge.protocol.CHANNEL_NAME
import com.iroselle.authman.bridge.protocol.MAX_ARGUMENTS
import com.iroselle.authman.bridge.protocol.MAX_ARGUMENT_LENGTH
import com.iroselle.authman.bridge.protocol.MAX_PAYLOAD_BYTES
import com.iroselle.authman.bridge.protocol.PROTOCOL_VERSION
import com.iroselle.authman.bridge.protocol.validRequestId
import org.bukkit.command.Command
import org.bukkit.command.CommandSender
import org.bukkit.command.ConsoleCommandSender
import org.bukkit.command.RemoteConsoleCommandSender
import org.bukkit.entity.Player
import org.bukkit.event.EventHandler
import org.bukkit.event.Listener
import org.bukkit.event.player.PlayerJoinEvent
import org.bukkit.plugin.ServicePriority
import org.bukkit.plugin.java.JavaPlugin
import org.bukkit.plugin.messaging.PluginMessageListener
import java.nio.charset.StandardCharsets
import java.util.UUID
import java.util.concurrent.CompletableFuture
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit

class AuthmanBridgePlugin : JavaPlugin(), AuthmanBridge, PluginMessageListener, Listener {
    private val gson = Gson()
    private val pending = ConcurrentHashMap<String, PendingRequest>()
    private val suggestionCache = ConcurrentHashMap<String, List<String>>()
    private val suggestionRequests = ConcurrentHashMap.newKeySet<String>()
    private lateinit var dispatcher: FoliaDispatcher

    @Volatile
    private var enabled = false

    @Volatile
    override var available: Boolean = false
        private set

    override fun onEnable() {
        dispatcher = FoliaDispatcher(this)
        server.messenger.registerOutgoingPluginChannel(this, CHANNEL_NAME)
        server.messenger.registerIncomingPluginChannel(this, CHANNEL_NAME, this)
        server.pluginManager.registerEvents(this, this)
        server.servicesManager.register(AuthmanBridge::class.java, this, this, ServicePriority.Normal)
        getCommand("authman")?.setExecutor(this)
        getCommand("authman")?.tabCompleter = this
        enabled = true
        server.onlinePlayers.forEach(::requestCapabilitiesSoon)
        logger.info("Authman bridge enabled; Core credentials remain owned by the Velocity bootstrap")
    }

    override fun onDisable() {
        enabled = false
        available = false
        server.servicesManager.unregister(AuthmanBridge::class.java, this)
        server.messenger.unregisterIncomingPluginChannel(this, CHANNEL_NAME, this)
        server.messenger.unregisterOutgoingPluginChannel(this, CHANNEL_NAME)
        val error = IllegalStateException("Authman bridge is disabled")
        pending.values.forEach { it.future.completeExceptionally(error) }
        pending.clear()
        suggestionCache.clear()
        suggestionRequests.clear()
    }

    override fun onCommand(sender: CommandSender, command: Command, label: String, args: Array<out String>): Boolean {
        execute(sender, args.toList()).whenComplete { result, error ->
            dispatcher.execute(sender) {
                if (error != null) {
                    sender.sendMessage(renderError(error.message ?: "Authman bridge request failed"))
                } else {
                    result.messages.forEach { sender.sendMessage(render(it)) }
                }
            }
        }
        return true
    }

    override fun onTabComplete(sender: CommandSender, command: Command, alias: String, args: Array<out String>): List<String> {
        val arguments = args.toList()
        val cacheKey = suggestionKey(sender, arguments)
        if (suggestionRequests.add(cacheKey)) {
            suggest(sender, arguments).whenComplete { suggestions, _ ->
                if (suggestions != null) {
                    suggestionCache[cacheKey] = suggestions
                }
                suggestionRequests.remove(cacheKey)
            }
        }
        val cached = suggestionCache[cacheKey]
        if (cached != null) {
            return cached
        }
        if (arguments.size <= 1) {
            val prefix = arguments.firstOrNull().orEmpty()
            return listOf("reload").filter { it.startsWith(prefix, ignoreCase = true) }
        }
        return emptyList()
    }

    override fun execute(sender: CommandSender, arguments: List<String>): CompletableFuture<AuthmanBridgeResult> =
        request(sender, "execute", arguments).thenApply { response ->
            AuthmanBridgeResult(
                success = response.success,
                messages = response.messages.map {
                    AuthmanBridgeMessage(
                        text = it.text,
                        tone = it.tone,
                        actionType = it.actionType,
                        actionValue = it.actionValue,
                    )
                },
            )
        }

    override fun suggest(sender: CommandSender, arguments: List<String>): CompletableFuture<List<String>> =
        request(sender, "suggest", arguments).thenApply { it.suggestions }

    override fun onPluginMessageReceived(channel: String, player: Player, message: ByteArray) {
        if (channel != CHANNEL_NAME || message.isEmpty() || message.size > MAX_PAYLOAD_BYTES) {
            return
        }
        val response = try {
            gson.fromJson(String(message, StandardCharsets.UTF_8), BridgeResponse::class.java)
        } catch (ex: Exception) {
            logger.warning("Ignored malformed Authman bridge response: ${ex.message}")
            return
        }
        if (response.version != PROTOCOL_VERSION) {
            logger.warning("Ignored Authman bridge protocol ${response.version}; expected $PROTOCOL_VERSION")
            return
        }
        available = response.available
        if (response.type == "state") {
            suggestionCache.clear()
            if (response.available) {
                requestCapabilitiesSoon(player)
            }
            return
        }
        if (!validRequestId(response.requestId)) {
            return
        }
        val request = pending.remove(response.requestId) ?: return
        if (response.type == "error") {
            request.future.completeExceptionally(IllegalStateException(response.error.ifBlank { "Authman bridge request failed" }))
            return
        }
        if (request.type == "capabilities" || request.type == "suggest") {
            suggestionCache[request.cacheKey] = response.suggestions
        }
        request.future.complete(response)
    }

    @EventHandler
    fun onPlayerJoin(event: PlayerJoinEvent) {
        requestCapabilitiesSoon(event.player)
    }

    private fun request(
        sender: CommandSender,
        type: String,
        arguments: List<String>,
    ): CompletableFuture<BridgeResponse> {
        if (!enabled) {
            return failedFuture("Authman bridge is disabled")
        }
        if (arguments.size > MAX_ARGUMENTS || arguments.any { it.length > MAX_ARGUMENT_LENGTH }) {
            return failedFuture("Authman command arguments exceed the bridge limit")
        }
        if (pending.size >= MAX_PENDING_REQUESTS) {
            return failedFuture("Authman bridge has too many pending requests")
        }
        val carrier = when (sender) {
            is Player -> sender.takeIf { it.isOnline }
            else -> server.onlinePlayers.firstOrNull()
        } ?: return failedFuture(
            "Authman bridge needs at least one player connected to this backend server because plugin messages use player connections.",
        )
        val wireSender = when (sender) {
            is Player -> BridgeSender(kind = "player", name = sender.name, uniqueId = sender.uniqueId.toString())
            is ConsoleCommandSender, is RemoteConsoleCommandSender -> BridgeSender(kind = "console", name = "console")
            else -> return failedFuture("This Bukkit command sender cannot use the Authman bridge")
        }
        val requestId = UUID.randomUUID().toString().replace("-", "")
        val request = BridgeRequest(
            type = type,
            requestId = requestId,
            sender = wireSender,
            arguments = arguments,
        )
        val payload = gson.toJson(request).toByteArray(StandardCharsets.UTF_8)
        if (payload.size > MAX_PAYLOAD_BYTES) {
            return failedFuture("Authman bridge request exceeds the plugin message limit")
        }
        val future = CompletableFuture<BridgeResponse>()
        val pendingRequest = PendingRequest(
            type = type,
            cacheKey = suggestionKey(sender, arguments),
            future = future,
        )
        pending[requestId] = pendingRequest
        val scheduled = dispatcher.execute(carrier) {
            if (!enabled) {
                if (pending.remove(requestId, pendingRequest)) {
                    future.completeExceptionally(IllegalStateException("Authman bridge is disabled"))
                }
                return@execute
            }
            try {
                carrier.sendPluginMessage(this, CHANNEL_NAME, payload)
            } catch (ex: Exception) {
                if (pending.remove(requestId, pendingRequest)) {
                    future.completeExceptionally(ex)
                }
                return@execute
            }
            CompletableFuture.delayedExecutor(REQUEST_TIMEOUT_SECONDS, TimeUnit.SECONDS).execute {
                if (pending.remove(requestId, pendingRequest)) {
                    future.completeExceptionally(IllegalStateException("Authman bridge request timed out"))
                }
            }
        }
        if (!scheduled) {
            pending.remove(requestId)
            future.completeExceptionally(IllegalStateException("Authman bridge carrier is no longer connected"))
        }
        return future
    }

    private fun requestCapabilitiesSoon(player: Player) {
        dispatcher.execute(player) {
            val cacheKey = actorKey(player) + "|root"
            if (!suggestionRequests.add(cacheKey)) {
                return@execute
            }
            request(player, "capabilities", listOf("")).whenComplete { response, _ ->
                if (response != null) {
                    suggestionCache[cacheKey] = response.suggestions
                }
                suggestionRequests.remove(cacheKey)
            }
        }
    }

    private fun suggestionKey(sender: CommandSender, arguments: List<String>): String =
        actorKey(sender) + "|" + arguments.joinToString("\u0000")

    private fun actorKey(sender: CommandSender): String = when (sender) {
        is Player -> "player:${sender.uniqueId}"
        else -> "console"
    }

    private fun render(message: AuthmanBridgeMessage): String {
        val color = when (message.tone.lowercase()) {
            "success" -> COLOR_GREEN
            "warning" -> COLOR_GOLD
            "error" -> COLOR_RED
            "muted" -> COLOR_GRAY
            else -> COLOR_AQUA
        }
        return "$COLOR_DARK_AQUA${COLOR_BOLD}Authman $COLOR_RESET$color${message.text}"
    }

    private fun renderError(message: String): String =
        "$COLOR_DARK_AQUA${COLOR_BOLD}Authman $COLOR_RESET$COLOR_RED$message"

    private fun <T> failedFuture(message: String): CompletableFuture<T> =
        CompletableFuture<T>().also { it.completeExceptionally(IllegalStateException(message)) }

    private data class PendingRequest(
        val type: String,
        val cacheKey: String,
        val future: CompletableFuture<BridgeResponse>,
    )

    companion object {
        private const val REQUEST_TIMEOUT_SECONDS = 8L
        private const val MAX_PENDING_REQUESTS = 128
        private const val COLOR_DARK_AQUA = "\u00a73"
        private const val COLOR_GREEN = "\u00a7a"
        private const val COLOR_AQUA = "\u00a7b"
        private const val COLOR_RED = "\u00a7c"
        private const val COLOR_GOLD = "\u00a76"
        private const val COLOR_GRAY = "\u00a77"
        private const val COLOR_BOLD = "\u00a7l"
        private const val COLOR_RESET = "\u00a7r"
    }
}
