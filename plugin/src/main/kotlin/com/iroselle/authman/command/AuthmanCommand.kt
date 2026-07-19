package com.iroselle.authman.command

import com.iroselle.authman.AuthmanPlugin
import com.iroselle.authman.spi.AuthmanCommandAction
import com.iroselle.authman.spi.AuthmanCommandActionType
import com.iroselle.authman.spi.AuthmanCommandActor
import com.iroselle.authman.spi.AuthmanCommandActorKind
import com.iroselle.authman.spi.AuthmanCommandDescriptor
import com.iroselle.authman.spi.AuthmanCommandMessage
import com.iroselle.authman.spi.AuthmanCommandProvider
import com.iroselle.authman.spi.AuthmanCommandTone
import org.slf4j.Logger

class AuthmanCommand(
    private val plugin: AuthmanPlugin,
    private val logger: Logger,
) : AuthmanCommandProvider {
    override fun descriptors(actor: AuthmanCommandActor): List<AuthmanCommandDescriptor> {
        val available = buildList {
            if (allowed(actor, PERMISSION_STATUS, PERMISSION_ADMIN)) {
                add(
                    AuthmanCommandDescriptor(
                        name = "status",
                        summary = "Inspect this downstream node and its loaded runtime.",
                        usage = "/authman status",
                        permission = PERMISSION_STATUS,
                    ),
                )
            }
            if (actor.kind == AuthmanCommandActorKind.PLAYER && allowed(actor, PERMISSION_ACCOUNT, PERMISSION_PORTAL_LINK, PERMISSION_ADMIN)) {
                add(
                    AuthmanCommandDescriptor(
                        name = "account",
                        aliases = listOf("me"),
                        summary = "Open a short-lived link for your own Authman account.",
                        usage = "/authman account",
                        permission = PERMISSION_ACCOUNT,
                    ),
                )
            }
            if (playerActions(actor).isNotEmpty()) {
                add(
                    AuthmanCommandDescriptor(
                        name = "player",
                        summary = "Manage an online player's profile, portal link, or transfer.",
                        usage = "/authman player <player> <link|rename|transfer> [value]",
                        permission = "authman.command.player.*",
                    ),
                )
            }
            if (allowed(actor, PERMISSION_BAN, PERMISSION_ADMIN)) {
                add(
                    AuthmanCommandDescriptor(
                        name = "ban",
                        summary = "Create a profile or passport ban through Core.",
                        usage = "/authman ban <profile|passport> <target> <duration> <reason>",
                        permission = PERMISSION_BAN,
                    ),
                )
            }
        }
        if (available.isEmpty()) {
            return emptyList()
        }
        return listOf(
            AuthmanCommandDescriptor(
                name = "help",
                summary = "Show the Authman capabilities available to this sender.",
                usage = "/authman help",
            ),
        ) + available
    }

    override fun execute(actor: AuthmanCommandActor, arguments: List<String>): Boolean {
        val action = arguments.firstOrNull()?.lowercase().orEmpty()
        return when (action) {
            "", "help" -> {
                showHelp(actor)
                true
            }
            "status" -> {
                if (!require(actor, "view Authman status", PERMISSION_STATUS, PERMISSION_ADMIN)) return true
                showStatus(actor)
                true
            }
            "account", "me" -> {
                if (actor.kind != AuthmanCommandActorKind.PLAYER) {
                    actor.error("The account command can only be used by a player. Use /authman player <player> link from console.")
                    return true
                }
                if (!require(actor, "open an account link", PERMISSION_ACCOUNT, PERMISSION_PORTAL_LINK, PERMISSION_ADMIN)) return true
                sendPortalLink(actor, actor.name)
                true
            }
            "player" -> {
                executePlayer(actor, arguments)
                true
            }
            "ban" -> {
                executeBan(actor, arguments)
                true
            }

            // Hidden compatibility paths for existing scripts and permission setups.
            "reconnect" -> {
                if (!require(actor, "reconnect Authman", PERMISSION_ADMIN)) return true
                val ok = plugin.reconnectNow()
                actor.send(
                    AuthmanCommandMessage(
                        if (ok) "Core connection is available." else "Core connection is unavailable.",
                        if (ok) AuthmanCommandTone.SUCCESS else AuthmanCommandTone.WARNING,
                    ),
                )
                true
            }
            "transfer" -> {
                if (!require(actor, "transfer players", PERMISSION_TRANSFER, PERMISSION_ADMIN)) return true
                if (arguments.size < 3) {
                    actor.usage("/authman player <player> transfer <downstream-server>")
                } else {
                    runOperation(actor, "transfer player") { plugin.transferPlayer(arguments[1], arguments[2]) }
                }
                true
            }
            "rename-profile" -> {
                if (!require(actor, "rename profiles", PERMISSION_PROFILE, PERMISSION_ADMIN)) return true
                if (arguments.size < 3) {
                    actor.usage("/authman player <player> rename <new-profile-name>")
                } else {
                    runOperation(actor, "rename profile") { plugin.renameCurrentProfile(arguments[1], arguments[2]) }
                }
                true
            }
            "portal-link" -> {
                if (!require(actor, "create portal links", PERMISSION_PORTAL_LINK, PERMISSION_ADMIN)) return true
                val target = arguments.getOrNull(1) ?: actor.name.takeIf { actor.kind == AuthmanCommandActorKind.PLAYER }
                if (target.isNullOrBlank()) actor.usage("/authman player <player> link") else sendPortalLink(actor, target)
                true
            }
            "ban-profile", "ban-passport" -> {
                if (!require(actor, "ban Authman identities", PERMISSION_BAN, PERMISSION_ADMIN)) return true
                executeLegacyBan(actor, action, arguments)
                true
            }
            else -> false
        }
    }

    override fun suggest(actor: AuthmanCommandActor, arguments: List<String>): List<String> {
        if (arguments.size <= 1) {
            val prefix = arguments.firstOrNull().orEmpty()
            return descriptors(actor)
                .flatMap { listOf(it.name) + it.aliases }
                .distinct()
                .filter { it.startsWith(prefix, ignoreCase = true) }
                .take(MAX_SUGGESTIONS)
        }
        return when (arguments[0].lowercase()) {
            "player" -> suggestPlayer(actor, arguments)
            "ban" -> suggestBan(actor, arguments)
            "transfer" -> when (arguments.size) {
                2 -> onlineSuggestions(arguments[1])
                3 -> plugin.downstreamTransferSuggestions(arguments[2])
                else -> emptyList()
            }
            "rename-profile", "portal-link" -> if (arguments.size == 2) onlineSuggestions(arguments[1]) else emptyList()
            "ban-profile", "ban-passport" -> if (arguments.size == 3) durationSuggestions(arguments[2]) else emptyList()
            else -> emptyList()
        }
    }

    private fun showHelp(actor: AuthmanCommandActor) {
        actor.send(AuthmanCommandMessage("Downstream controls available on this node", AuthmanCommandTone.SUCCESS))
        descriptors(actor).forEach { descriptor ->
            actor.send(
                AuthmanCommandMessage(
                    "${descriptor.usage}  ${descriptor.summary}",
                    AuthmanCommandTone.INFO,
                    AuthmanCommandAction(AuthmanCommandActionType.SUGGEST_COMMAND, commandSeed(descriptor.usage)),
                ),
            )
        }
        if (allowed(actor, PERMISSION_RELOAD, PERMISSION_ADMIN)) {
            actor.send(
                AuthmanCommandMessage(
                    "/authman reload  Reload the bootstrap configuration and reconnect to Core.",
                    AuthmanCommandTone.MUTED,
                    AuthmanCommandAction(AuthmanCommandActionType.SUGGEST_COMMAND, "/authman reload"),
                ),
            )
        }
    }

    private fun showStatus(actor: AuthmanCommandActor) {
        val status = plugin.commandStatus()
        val label = status.nodeName.ifBlank { "Velocity" }
        val serverId = status.serverId.ifBlank { "unassigned" }
        actor.send(AuthmanCommandMessage("$label is connected to Core.", AuthmanCommandTone.SUCCESS))
        actor.send(AuthmanCommandMessage("Runtime ${status.runtimeVersion} · server $serverId", AuthmanCommandTone.INFO))
        actor.send(
            AuthmanCommandMessage(
                "${status.onlinePlayers}/${status.maxPlayers} downstream players · ${status.transferTargets} transfer targets",
                AuthmanCommandTone.MUTED,
            ),
        )
    }

    private fun executePlayer(actor: AuthmanCommandActor, arguments: List<String>) {
        val available = playerActions(actor)
        if (available.isEmpty()) {
            actor.error("You do not have permission to manage Authman players.")
            return
        }
        if (arguments.size < 3) {
            actor.usage("/authman player <player> <${available.joinToString("|")}> [value]")
            return
        }
        val target = arguments[1]
        when (arguments[2].lowercase()) {
            "link" -> {
                if (!require(actor, "create portal links", PERMISSION_PORTAL_LINK, PERMISSION_ADMIN)) return
                sendPortalLink(actor, target)
            }
            "rename" -> {
                if (!require(actor, "rename profiles", PERMISSION_PROFILE, PERMISSION_ADMIN)) return
                val next = arguments.getOrNull(3)
                if (next.isNullOrBlank()) {
                    actor.usage("/authman player <player> rename <new-profile-name>")
                    return
                }
                runOperation(actor, "rename profile") { plugin.renameCurrentProfile(target, next) }
            }
            "transfer" -> {
                if (!require(actor, "transfer players", PERMISSION_TRANSFER, PERMISSION_ADMIN)) return
                val destination = arguments.getOrNull(3)
                if (destination.isNullOrBlank()) {
                    actor.usage("/authman player <player> transfer <downstream-server>")
                    return
                }
                runOperation(actor, "transfer player") { plugin.transferPlayer(target, destination) }
            }
            else -> actor.usage("/authman player <player> <${available.joinToString("|")}> [value]")
        }
    }

    private fun executeBan(actor: AuthmanCommandActor, arguments: List<String>) {
        if (!require(actor, "ban Authman identities", PERMISSION_BAN, PERMISSION_ADMIN)) return
        if (arguments.size < 5) {
            actor.usage("/authman ban <profile|passport> <target> <duration> <reason>")
            actor.send(AuthmanCommandMessage("Durations: 1s, 1min, 1h, 1d, 1w, 1m, 1y.", AuthmanCommandTone.MUTED))
            return
        }
        val scope = arguments[1].lowercase()
        val target = arguments[2]
        val duration = parseDurationSeconds(arguments[3])
        if (duration == null) {
            actor.error("Invalid duration. Use values such as 1h, 1w, 1m, or 1y.")
            return
        }
        val reason = arguments.drop(4).joinToString(" ").trim()
        if (reason.isBlank()) {
            actor.error("A ban reason is required.")
            return
        }
        runOperation(actor, "create ban") {
            when (scope) {
                "profile" -> plugin.client().banProfile(target, duration, reason)
                "passport" -> plugin.client().banPassport(target, duration, reason)
                else -> throw IllegalArgumentException("ban scope must be profile or passport")
            }
            "Created a $scope ban for $target."
        }
    }

    private fun executeLegacyBan(actor: AuthmanCommandActor, action: String, arguments: List<String>) {
        if (arguments.size < 4) {
            actor.usage("/authman ban <profile|passport> <target> <duration> <reason>")
            return
        }
        val scope = if (action == "ban-profile") "profile" else "passport"
        executeBan(actor, listOf("ban", scope, arguments[1], arguments[2]) + arguments.drop(3))
    }

    private fun sendPortalLink(actor: AuthmanCommandActor, playerName: String) {
        try {
            val result = plugin.createPortalLink(playerName)
            actor.send(AuthmanCommandMessage("Created a short-lived account link for $playerName.", AuthmanCommandTone.SUCCESS))
            actor.send(
                AuthmanCommandMessage(
                    result.url,
                    AuthmanCommandTone.INFO,
                    AuthmanCommandAction(AuthmanCommandActionType.OPEN_URL, result.url),
                ),
            )
        } catch (ex: Exception) {
            operationFailed(actor, "create portal link", ex)
        }
    }

    private fun runOperation(actor: AuthmanCommandActor, label: String, action: () -> String) {
        try {
            actor.send(AuthmanCommandMessage(action(), AuthmanCommandTone.SUCCESS))
        } catch (ex: Exception) {
            operationFailed(actor, label, ex)
        }
    }

    private fun operationFailed(actor: AuthmanCommandActor, label: String, error: Exception) {
        plugin.lockIfCoreRejected(error)
        if (error !is IllegalArgumentException) {
            logger.warn("Failed to {} for command actor {}", label, actor.name, error)
        }
        actor.error("Failed to $label: ${error.message?.take(300) ?: "unknown error"}")
    }

    private fun suggestPlayer(actor: AuthmanCommandActor, arguments: List<String>): List<String> = when (arguments.size) {
        2 -> onlineSuggestions(arguments[1])
        3 -> playerActions(actor).filter { it.startsWith(arguments[2], ignoreCase = true) }
        4 -> if (arguments[2].equals("transfer", ignoreCase = true)) plugin.downstreamTransferSuggestions(arguments[3]) else emptyList()
        else -> emptyList()
    }

    private fun suggestBan(actor: AuthmanCommandActor, arguments: List<String>): List<String> {
        if (!allowed(actor, PERMISSION_BAN, PERMISSION_ADMIN)) return emptyList()
        return when (arguments.size) {
            2 -> listOf("profile", "passport").filter { it.startsWith(arguments[1], ignoreCase = true) }
            3 -> onlineSuggestions(arguments[2])
            4 -> durationSuggestions(arguments[3])
            else -> emptyList()
        }
    }

    private fun onlineSuggestions(prefix: String): List<String> =
        plugin.onlinePlayerNames().filter { it.startsWith(prefix, ignoreCase = true) }.take(MAX_SUGGESTIONS)

    private fun durationSuggestions(prefix: String): List<String> =
        listOf("1h", "1d", "1w", "1m", "1y").filter { it.startsWith(prefix, ignoreCase = true) }

    private fun playerActions(actor: AuthmanCommandActor): List<String> = buildList {
        if (allowed(actor, PERMISSION_PORTAL_LINK, PERMISSION_ADMIN)) add("link")
        if (allowed(actor, PERMISSION_PROFILE, PERMISSION_ADMIN)) add("rename")
        if (allowed(actor, PERMISSION_TRANSFER, PERMISSION_ADMIN)) add("transfer")
    }

    private fun require(actor: AuthmanCommandActor, action: String, vararg permissions: String): Boolean {
        if (allowed(actor, *permissions)) return true
        actor.error("You do not have permission to $action.")
        return false
    }

    private fun allowed(actor: AuthmanCommandActor, vararg permissions: String): Boolean =
        actor.kind != AuthmanCommandActorKind.PLAYER ||
            actor.hasPermission(PERMISSION_ALL) ||
            permissions.any(actor::hasPermission)

    private fun commandSeed(usage: String): String {
        val marker = listOf(usage.indexOf('<'), usage.indexOf('[')).filter { it >= 0 }.minOrNull()
        val base = if (marker == null) usage else usage.substring(0, marker)
        return base.trimEnd() + if (marker == null) "" else " "
    }

    companion object {
        const val PERMISSION_ALL = "authman.command.*"
        const val PERMISSION_ADMIN = "authman.command.admin"
        const val PERMISSION_RELOAD = "authman.command.reload"
        const val PERMISSION_STATUS = "authman.command.status"
        const val PERMISSION_ACCOUNT = "authman.command.account"
        const val PERMISSION_BAN = "authman.command.ban"
        const val PERMISSION_PROFILE = "authman.command.profile"
        const val PERMISSION_TRANSFER = "authman.command.transfer"
        const val PERMISSION_PORTAL_LINK = "authman.command.portal-link"
        private const val MAX_SUGGESTIONS = 25
        private val DURATION_PATTERN = Regex("""^([1-9][0-9]*)(s|min|h|d|w|m|y)$""")

        fun parseDurationSeconds(input: String): Long? {
            val match = DURATION_PATTERN.matchEntire(input.lowercase()) ?: return null
            val amount = match.groupValues[1].toLongOrNull() ?: return null
            val multiplier = when (match.groupValues[2]) {
                "s" -> 1L
                "min" -> 60L
                "h" -> 60L * 60L
                "d" -> 24L * 60L * 60L
                "w" -> 7L * 24L * 60L * 60L
                "m" -> 30L * 24L * 60L * 60L
                "y" -> 365L * 24L * 60L * 60L
                else -> return null
            }
            return runCatching { Math.multiplyExact(amount, multiplier) }.getOrNull()
        }
    }
}

private fun AuthmanCommandActor.error(text: String) {
    send(AuthmanCommandMessage(text, AuthmanCommandTone.ERROR))
}

private fun AuthmanCommandActor.usage(text: String) {
    send(
        AuthmanCommandMessage(
            "Usage: $text",
            AuthmanCommandTone.WARNING,
            AuthmanCommandAction(AuthmanCommandActionType.SUGGEST_COMMAND, text.substringBefore('<').trimEnd() + " "),
        ),
    )
}
