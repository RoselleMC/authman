package mc.roselle.authman.message

import com.velocitypowered.api.proxy.Player
import mc.roselle.authman.config.AuthmanConfig
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import net.kyori.adventure.text.format.TextDecoration

class AuthmanMessages(private val config: AuthmanConfig) {
    fun sendTemporaryUnavailable(player: Player) {
        player.sendMessage(Component.text("Authman 暂时不可用，请稍后重试 / Authman is temporarily unavailable.", NamedTextColor.RED))
    }

    fun temporaryUnavailable(): Component =
        Component.text("Authman 暂时不可用，请稍后重试。\nAuthman is temporarily unavailable.", NamedTextColor.RED)

    fun locked(): Component =
        Component.text("This Authman account is locked.\n该 Authman 账号已锁定。", NamedTextColor.RED)

    private fun sendHeading(player: Player, english: String, chinese: String, suffix: Component) {
        player.sendMessage(
            Component.text("  ")
                .append(Component.text(english, NamedTextColor.GREEN, TextDecoration.BOLD))
                .append(Component.text(" / $chinese", NamedTextColor.DARK_GREEN, TextDecoration.BOLD))
                .append(Component.space())
                .append(suffix),
        )
    }

    private fun indent(message: String, color: NamedTextColor = NamedTextColor.GRAY): Component =
        Component.text("    ").append(Component.text(message, color))

    private fun indicator(message: String, color: NamedTextColor): Component =
        Component.text("[").color(NamedTextColor.DARK_GRAY)
            .append(Component.text(message, color, TextDecoration.BOLD))
            .append(Component.text("]", NamedTextColor.DARK_GRAY))
}
