package com.iroselle.authman.bootstrap

import com.velocitypowered.api.event.PostOrder
import com.velocitypowered.api.event.ResultedEvent
import com.velocitypowered.api.event.Subscribe
import com.velocitypowered.api.event.connection.LoginEvent
import com.velocitypowered.api.event.player.GameProfileRequestEvent
import com.velocitypowered.api.proxy.InboundConnection
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.format.NamedTextColor
import java.util.Locale
import java.util.concurrent.ConcurrentHashMap

class BootstrapGate internal constructor(
    private val admissionGeneration: () -> Long?,
    private val nanoTime: () -> Long,
) {
    constructor(manager: RuntimeManager) : this(manager::admissionGeneration, System::nanoTime)

    private val attempts = ConcurrentHashMap<ConnectionKey, Attempt>()

    @Subscribe(order = PostOrder.FIRST)
    fun onGameProfileRequest(event: GameProfileRequestEvent) {
        val now = nanoTime()
        purgeExpired(now)
        val key = connectionKey(event.connection)
        if (attempts.size >= MAX_TRACKED_ATTEMPTS && !attempts.containsKey(key)) {
            return
        }
        attempts[key] = Attempt(admissionGeneration(), now)
    }

    @Subscribe(order = PostOrder.LAST)
    fun onLogin(event: LoginEvent) {
        val current = admissionGeneration()
        val attempt = attempts.remove(connectionKey(event.player))
        val attemptGeneration = attempt?.generation
        val elapsed = attempt?.let { nanoTime() - it.createdAt }
        val fresh = elapsed != null && elapsed in 0..ATTEMPT_TTL_NANOS
        if (!fresh || current == null || attemptGeneration == null || attemptGeneration != current) {
            event.setResult(ResultedEvent.ComponentResult.denied(UNAVAILABLE))
        }
    }

    private fun purgeExpired(now: Long) {
        val cutoff = now - ATTEMPT_TTL_NANOS
        attempts.entries.removeIf { it.value.createdAt < cutoff }
    }

    private fun connectionKey(connection: InboundConnection): ConnectionKey {
        val remote = connection.remoteAddress
        val host = remote.address?.hostAddress ?: remote.hostString.lowercase(Locale.ROOT)
        return ConnectionKey(host, remote.port)
    }

    private data class ConnectionKey(val host: String, val port: Int)
    private data class Attempt(val generation: Long?, val createdAt: Long)

    companion object {
        private val UNAVAILABLE = Component.text(
            "Authman runtime is updating or unavailable. Please try again shortly.\nAuthman 核心逻辑正在更新或暂不可用，请稍后重试。",
            NamedTextColor.RED,
        )
        private const val ATTEMPT_TTL_NANOS = 30L * 1_000_000_000L
        private const val MAX_TRACKED_ATTEMPTS = 65_536
    }
}
