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
        const val KEY_ALREADY_ONLINE = "gate.kick.already_online"
        const val KEY_LOCKED = "gate.kick.locked"
        const val KEY_BANNED = "gate.kick.banned"
        const val KEY_DEFAULT_DISCONNECT = "gate.kick.default_disconnect"

        // Must mirror internal/playermsg defaults in the Go backend.
        private val DEFAULTS = mapOf(
            KEY_UNAVAILABLE to "<red>Authman 暂时不可用，请稍后重试。<newline>Authman is temporarily unavailable.</red>",
            KEY_ALREADY_ONLINE to "<red>This profile is already online on this server.<newline>该档案已在此下游服务器在线。</red><newline><gray>If this is stale, Authman is refreshing the status now. Please try again shortly.</gray>",
            KEY_LOCKED to "<red>This Authman account is locked.<newline>该 Authman 账号已锁定。</red>",
            KEY_BANNED to "<red>You are banned from this server.</red><newline><gray>{reason}</gray>",
            KEY_DEFAULT_DISCONNECT to "Authman disconnected this session.",
        )
    }
}
