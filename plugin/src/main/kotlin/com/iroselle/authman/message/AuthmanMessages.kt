package com.iroselle.authman.message

import java.util.concurrent.atomic.AtomicReference
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.minimessage.MiniMessage

/**
 * Player-visible gate messages. Sources are MiniMessage strings delivered by
 * Authman Core through the node heartbeat; built-in defaults keep every flow
 * working when Core is unreachable or a key is unset.
 */
class AuthmanMessages {
    private val mini = MiniMessage.miniMessage()
    private val overrides = AtomicReference<Map<String, String>>(emptyMap())

    fun apply(messages: Map<String, String>) {
        overrides.set(messages.filterValues { it.isNotBlank() })
    }

    fun temporaryUnavailable(): Component = render(KEY_UNAVAILABLE, emptyMap())

    fun missingTransferGrant(playerName: String = ""): Component =
        render(KEY_MISSING_TRANSFER_GRANT, mapOf("player" to playerName))

    fun transferUnsupported(playerName: String = ""): Component =
        render(KEY_TRANSFER_UNSUPPORTED, mapOf("player" to playerName))

    fun validationTimeout(playerName: String = ""): Component =
        render(KEY_VALIDATION_TIMEOUT, mapOf("player" to playerName))

    fun downstreamUnavailable(playerName: String = "", serverName: String = ""): Component =
        render(KEY_DOWNSTREAM_UNAVAILABLE, mapOf("player" to playerName, "server" to serverName))

    fun alreadyOnline(playerName: String = ""): Component = render(KEY_ALREADY_ONLINE, mapOf("player" to playerName))

    fun locked(playerName: String = ""): Component = render(KEY_LOCKED, mapOf("player" to playerName))

    fun banned(reason: String, playerName: String = ""): Component =
        render(KEY_BANNED, mapOf("reason" to reason, "player" to playerName))

    fun defaultDisconnect(playerName: String = ""): Component =
        render(KEY_DEFAULT_DISCONNECT, mapOf("player" to playerName))

    private fun render(key: String, vars: Map<String, String>): Component {
        val source = overrides.get()[key] ?: DEFAULTS.getValue(key)
        var resolved = source
        for ((name, value) in vars) {
            resolved = resolved.replace("{$name}", sanitize(value))
        }
        return try {
            mini.deserialize(resolved)
        } catch (_: Exception) {
            Component.text(resolved.replace("<newline>", "\n"))
        }
    }

    private fun sanitize(value: String): String = value.replace("<", "").replace(">", "")

    companion object {
        const val KEY_UNAVAILABLE = "gate.kick.unavailable"
        const val KEY_MISSING_TRANSFER_GRANT = "gate.kick.missing_transfer_grant"
        const val KEY_TRANSFER_UNSUPPORTED = "gate.kick.transfer_unsupported"
        const val KEY_VALIDATION_TIMEOUT = "gate.kick.validation_timeout"
        const val KEY_DOWNSTREAM_UNAVAILABLE = "gate.kick.downstream_unavailable"
        const val KEY_ALREADY_ONLINE = "gate.kick.already_online"
        const val KEY_LOCKED = "gate.kick.locked"
        const val KEY_BANNED = "gate.kick.banned"
        const val KEY_DEFAULT_DISCONNECT = "gate.kick.default_disconnect"

        // Must mirror internal/playermsg defaults in the Go backend.
        private val DEFAULTS = mapOf(
            KEY_UNAVAILABLE to "<red>Authman 暂时不可用，请稍后重试。<newline>Authman is temporarily unavailable.</red>",
            KEY_MISSING_TRANSFER_GRANT to "<red>Please join through the Authman login portal.<newline>请从 Authman 登录门户进入。</red><newline><gray>This downstream server only accepts Authman transfer sessions.</gray>",
            KEY_TRANSFER_UNSUPPORTED to "<red>Your client does not support Authman transfer cookies.<newline>当前客户端不支持 Authman 转送票据。</red><newline><gray>Please use Minecraft 1.20.5 or newer.</gray>",
            KEY_VALIDATION_TIMEOUT to "<red>Authman did not receive the transfer ticket in time.<newline>Authman 未能及时收到转送票据。</red><newline><gray>Please return to the login portal and try again.</gray>",
            KEY_DOWNSTREAM_UNAVAILABLE to "<red>The downstream server is currently unavailable.<newline>目标下游服务器当前不可用。</red><newline><gray>Please try {server} again later.</gray>",
            KEY_ALREADY_ONLINE to "<red>This profile is already online on this server.<newline>该档案已在此下游服务器在线。</red><newline><gray>If this is stale, Authman is refreshing the status now. Please try again shortly.</gray>",
            KEY_LOCKED to "<red>This Authman account is locked.<newline>该 Authman 账号已锁定。</red>",
            KEY_BANNED to "<red>You are banned from this server.</red><newline><gray>{reason}</gray>",
            KEY_DEFAULT_DISCONNECT to "Authman disconnected this session.",
        )
    }
}
