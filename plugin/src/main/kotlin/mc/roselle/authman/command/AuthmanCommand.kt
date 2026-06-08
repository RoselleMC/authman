package mc.roselle.authman.command

import com.velocitypowered.api.command.SimpleCommand
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
        if (!source.hasPermission(PERMISSION)) {
            source.sendMessage(Component.text("You do not have permission to use Authman moderation commands."))
            return
        }
        when (args.firstOrNull()?.lowercase()) {
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
        }
        if (args.size < 4) {
            source.sendMessage(Component.text("Usage: /authman <reload|reconnect|ban-profile|ban-passport> [player] [duration] [reason...]"))
            source.sendMessage(Component.text("Duration examples: 1s, 1min, 1h, 1w, 1m, 1y."))
            return
        }
        val action = args[0].lowercase()
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
                    source.sendMessage(Component.text("Usage: /authman <reload|reconnect|ban-profile|ban-passport> [player] [duration] [reason...]"))
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
            return listOf("reload", "reconnect", "ban-profile", "ban-passport").filter { it.startsWith(args.firstOrNull() ?: "") }
        }
        if (args.size == 3) {
            return listOf("1h", "1w", "1m", "1y").filter { it.startsWith(args[2]) }
        }
        return emptyList()
    }

    override fun hasPermission(invocation: SimpleCommand.Invocation): Boolean {
        return invocation.source().hasPermission(PERMISSION)
    }

    companion object {
        const val PERMISSION = "authman.command.ban"
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
