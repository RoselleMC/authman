package mc.roselle.authman.message

import com.velocitypowered.api.proxy.Player
import mc.roselle.authman.config.AuthmanConfig
import mc.roselle.authman.model.PlayerAuthSession
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import net.kyori.adventure.text.format.TextDecoration
import net.kyori.adventure.title.Title
import java.time.Duration

class AuthmanMessages(private val config: AuthmanConfig) {
    fun sendPasswordPrompt(player: Player, session: PlayerAuthSession) {
        player.showTitle(
            Title.title(
                Component.text("Authman 身份验证", NamedTextColor.GREEN, TextDecoration.BOLD),
                Component.text("按聊天键输入离线账号密码", NamedTextColor.WHITE),
                Title.Times.times(Duration.ZERO, Duration.ofSeconds(10), Duration.ZERO),
            ),
        )

        repeat(2) { player.sendMessage(Component.empty()) }
        val triesLeft = (config.maxPasswordAttempts - session.wrongPasswordCount).coerceAtLeast(0)
        val indicator = when (session.lastInputMarker) {
            "/wrongpassword" -> indicator("密码错误，剩余 $triesLeft 次 / Wrong password", NamedTextColor.RED)
            "/empty" -> indicator("密码不能为空 / Password required", NamedTextColor.RED)
            else -> Component.empty()
        }
        sendHeading(player, "Authman login required", "离线账号登录", indicator)
        player.sendMessage(indent("按 ").append(Component.keybind("key.chat", NamedTextColor.WHITE, TextDecoration.BOLD)).append(Component.text(" 输入密码；本条消息不会进入服务器聊天", NamedTextColor.GRAY)))
        player.sendMessage(indent("验证完成前会停留在当前服务器，目标服务器将在通过后自动连接"))
        player.sendMessage(indent("忘记密码时请通过 Authman 门户或服务器提供的邮箱找回入口处理", NamedTextColor.YELLOW))
        player.sendMessage(Component.empty())
    }

    fun sendSuccess(player: Player) {
        player.showTitle(
            Title.title(
                Component.text("验证完成", NamedTextColor.GREEN, TextDecoration.BOLD),
                Component.text("正在连接目标服务器...", NamedTextColor.WHITE),
                Title.Times.times(Duration.ZERO, Duration.ofSeconds(3), Duration.ofMillis(500)),
            ),
        )
        sendHeading(player, "Authman login successful", "验证完成", indicator("正在连接 / Connecting", NamedTextColor.GREEN))
    }

    fun sendEmailPrompt(player: Player) {
        player.showTitle(
            Title.title(
                Component.text("邮箱验证", NamedTextColor.AQUA, TextDecoration.BOLD),
                Component.text("请在聊天中输入邮箱地址", NamedTextColor.WHITE),
                Title.Times.times(Duration.ZERO, Duration.ofSeconds(10), Duration.ZERO),
            ),
        )
        sendHeading(player, "Authman email verification", "邮箱验证", Component.empty())
        player.sendMessage(indent("此流程由 Authman API 统一处理，不复用旧脚本的迁移或外部账号依赖"))
        player.sendMessage(indent("验证码会发送到玩家邮箱，用于注册确认、找回密码或高强度验证", NamedTextColor.YELLOW))
        player.sendMessage(Component.empty())
    }

    fun sendEmailCodePrompt(player: Player) {
        sendHeading(player, "Authman email code", "请输入邮箱验证码", Component.empty())
        player.sendMessage(indent("按 ").append(Component.keybind("key.chat", NamedTextColor.WHITE, TextDecoration.BOLD)).append(Component.text(" 输入验证码；完成后继续 Authman 登录流程", NamedTextColor.GRAY)))
        player.sendMessage(Component.empty())
    }

    fun sendTemporaryUnavailable(player: Player) {
        player.sendMessage(Component.text("Authman 暂时不可用，请稍后重试 / Authman is temporarily unavailable.", NamedTextColor.RED))
    }

    fun sendCooldownIgnored(player: Player) {
        player.sendMessage(Component.text("操作过快，请稍后再试 / Please wait before trying again.", NamedTextColor.YELLOW))
    }

    fun locked(): Component =
        Component.text("This Authman account is locked.\n该 Authman 账号已锁定。", NamedTextColor.RED)

    fun tooManyWrongPasswords(): Component =
        Component.text("密码错误次数过多，请重新进入服务器后再试。\nPassword failed too many times. Please rejoin and try again.", NamedTextColor.RED)

    fun authTimeout(): Component =
        Component.text("Authman 登录超时，请重新连接。\nAuthman login timed out. Please reconnect.", NamedTextColor.RED)

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
