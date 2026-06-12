package mc.roselle.authman.command

import com.velocitypowered.api.command.CommandSource
import com.velocitypowered.api.command.SimpleCommand
import com.velocitypowered.api.proxy.Player
import mc.roselle.authman.AuthmanPlugin
import net.kyori.adventure.text.Component
import org.slf4j.Logger

class AuthmanCommand(
    private val plugin: AuthmanPlugin,
    private val logger: Logger,
) : SimpleCommand {
    override fun execute(invocation: SimpleCommand.Invocation) {
        val source = invocation.source()
        val args = invocation.arguments()
        val action = args.firstOrNull()?.lowercase() ?: ""
        if (!hasActionPermission(source, action)) {
            source.sendMessage(Component.text("You do not have permission to use this Authman command."))
            return
        }
        when (action) {
            "reload" -> {
                val ok = plugin.reloadConfigAndReconnect()
                source.sendMessage(Component.text(if (ok) "Authman config reloaded and Core connection is available." else "Authman config reloaded, but Core connection is still unavailable."))
                return
            }
            "reconnect" -> {
                val ok = plugin.reconnectNow()
                source.sendMessage(Component.text(if (ok) "Authman Core connection is available." else "Authman Core connection is unavailable."))
                return
            }
            "transfer" -> {
                if (args.size < 3) {
                    source.sendMessage(Component.text("Usage: /authman transfer <player> <downstream-server>"))
                    return
                }
                try {
                    source.sendMessage(Component.text(plugin.transferPlayer(args[1], args[2])))
                } catch (ex: IllegalArgumentException) {
                    source.sendMessage(Component.text("Authman transfer failed: ${ex.message ?: "invalid command arguments"}"))
                } catch (ex: Exception) {
                    logger.warn("Failed to create Authman transfer for {} to {}", args[1], args[2], ex)
                    source.sendMessage(Component.text("Authman transfer failed: ${ex.message ?: "unknown error"}"))
                }
                return
            }
        }
        if (args.size < 4) {
            source.sendMessage(Component.text("Usage: /authman <reload|reconnect|transfer|ban-profile|ban-passport> [args...]"))
            source.sendMessage(Component.text("Duration examples: 1s, 1min, 1h, 1w, 1m, 1y."))
            return
        }
        val target = args[1]
        val durationSeconds = parseDurationSeconds(args[2])
        if (durationSeconds == null) {
            source.sendMessage(Component.text("Invalid duration. Use examples like 1s, 1min, 1h, 1w, 1m, 1y."))
            return
        }
        val reason = args.drop(3).joinToString(" ").trim()
        if (reason.isBlank()) {
            source.sendMessage(Component.text("A ban reason is required."))
            return
        }
        try {
            when (action) {
                "ban-profile" -> plugin.client().banProfile(target, durationSeconds, reason)
                "ban-passport" -> plugin.client().banPassport(target, durationSeconds, reason)
                else -> {
                    source.sendMessage(Component.text("Usage: /authman <reload|reconnect|transfer|ban-profile|ban-passport> [args...]"))
                    return
                }
            }
            source.sendMessage(Component.text("Authman ban created for $target."))
        } catch (ex: Exception) {
            logger.warn("Failed to create Authman ban for {}", target, ex)
            source.sendMessage(Component.text("Authman ban failed: ${ex.message ?: "unknown error"}"))
        }
    }

    override fun suggest(invocation: SimpleCommand.Invocation): List<String> {
        val args = invocation.arguments()
        if (args.size <= 1) {
            return listOf("reload", "reconnect", "transfer", "ban-profile", "ban-passport")
                .filter { hasActionPermission(invocation.source(), it) }
                .filter { it.startsWith(args.firstOrNull() ?: "") }
        }
        val action = args[0].lowercase()
        if (action == "transfer") {
            if (args.size == 2) {
                return plugin.onlinePlayerNames().filter { it.startsWith(args[1], ignoreCase = true) }.take(25)
            }
            if (args.size == 3) {
                return plugin.downstreamTransferSuggestions(args[2])
            }
            return emptyList()
        }
        if (args.size == 3) {
            return listOf("1h", "1w", "1m", "1y").filter { it.startsWith(args[2]) }
        }
        return emptyList()
    }

    override fun hasPermission(invocation: SimpleCommand.Invocation): Boolean {
        return invocation.source() !is Player ||
            invocation.source().hasPermission(PERMISSION_ALL) ||
            invocation.source().hasPermission(PERMISSION_ADMIN) ||
            invocation.source().hasPermission(PERMISSION_BAN) ||
            invocation.source().hasPermission(PERMISSION_TRANSFER)
    }

    private fun hasActionPermission(source: CommandSource, action: String): Boolean {
        if (source !is Player) {
            return true
        }
        if (source.hasPermission(PERMISSION_ALL)) {
            return true
        }
        return when (action) {
            "reload", "reconnect" -> source.hasPermission(PERMISSION_ADMIN)
            "ban-profile", "ban-passport" -> source.hasPermission(PERMISSION_BAN)
            "transfer" -> source.hasPermission(PERMISSION_TRANSFER)
            else -> source.hasPermission(PERMISSION_ADMIN) || source.hasPermission(PERMISSION_BAN) || source.hasPermission(PERMISSION_TRANSFER)
        }
    }

    companion object {
        const val PERMISSION_ALL = "authman.command.*"
        const val PERMISSION_ADMIN = "authman.command.admin"
        const val PERMISSION_BAN = "authman.command.ban"
        const val PERMISSION_TRANSFER = "authman.command.transfer"
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
            return amount * multiplier
        }
    }
}
