package com.iroselle.authman.bootstrap

import com.iroselle.authman.spi.AuthmanCommandAction
import com.iroselle.authman.spi.AuthmanCommandActionType
import com.iroselle.authman.spi.AuthmanCommandActor
import com.iroselle.authman.spi.AuthmanCommandActorKind
import com.iroselle.authman.spi.AuthmanCommandDescriptor
import com.iroselle.authman.spi.AuthmanCommandMessage
import com.iroselle.authman.spi.AuthmanCommandProvider
import com.iroselle.authman.spi.AuthmanCommandRegistration
import com.iroselle.authman.spi.AuthmanCommandRegistry
import com.iroselle.authman.spi.AuthmanCommandTone
import com.iroselle.authman.spi.RuntimeControl
import com.velocitypowered.api.command.CommandSource
import com.velocitypowered.api.proxy.Player
import net.kyori.adventure.text.Component
import net.kyori.adventure.text.event.ClickEvent
import net.kyori.adventure.text.event.HoverEvent
import net.kyori.adventure.text.format.NamedTextColor
import net.kyori.adventure.text.format.TextDecoration
import org.slf4j.Logger
import java.util.concurrent.CopyOnWriteArraySet
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.locks.ReentrantReadWriteLock

class RuntimeCommandRouter(
    private val logger: Logger,
    private val control: RuntimeControl,
) : AuthmanCommandRegistry {
    private val lock = ReentrantReadWriteLock()
    private val listeners = CopyOnWriteArraySet<() -> Unit>()

    @Volatile
    private var provider: AuthmanCommandProvider? = null

    @Volatile
    private var runtimeReady = false

    @Volatile
    private var coreConnected = false

    override fun install(provider: AuthmanCommandProvider): AuthmanCommandRegistration {
        lock.writeLock().lock()
        try {
            check(this.provider == null) { "an Authman runtime command provider is already installed" }
            this.provider = provider
        } finally {
            lock.writeLock().unlock()
        }
        notifyChanged()
        val closed = AtomicBoolean(false)
        return object : AuthmanCommandRegistration {
            override fun close() {
                if (!closed.compareAndSet(false, true)) {
                    return
                }
                lock.writeLock().lock()
                try {
                    if (this@RuntimeCommandRouter.provider === provider) {
                        this@RuntimeCommandRouter.provider = null
                    }
                } finally {
                    lock.writeLock().unlock()
                }
                notifyChanged()
            }
        }
    }

    fun setCoreConnected(connected: Boolean) {
        if (coreConnected != connected) {
            coreConnected = connected
            notifyChanged()
        }
    }

    fun suspendRuntime() {
        var changed = false
        lock.writeLock().lock()
        try {
            if (runtimeReady) {
                runtimeReady = false
                changed = true
            }
        } finally {
            lock.writeLock().unlock()
        }
        if (changed) {
            notifyChanged()
        }
    }

    fun resumeRuntime() {
        var changed = false
        lock.writeLock().lock()
        try {
            if (!runtimeReady) {
                runtimeReady = true
                changed = true
            }
        } finally {
            lock.writeLock().unlock()
        }
        if (changed) {
            notifyChanged()
        }
    }

    fun isOperational(): Boolean {
        lock.readLock().lock()
        return try {
            runtimeReady && coreConnected && provider != null
        } finally {
            lock.readLock().unlock()
        }
    }

    fun addChangeListener(listener: () -> Unit): AutoCloseable {
        listeners += listener
        return AutoCloseable { listeners -= listener }
    }

    fun velocityActor(source: CommandSource): AuthmanCommandActor = VelocityCommandActor(source)

    fun canUse(actor: AuthmanCommandActor): Boolean {
        if (canReload(actor)) {
            return true
        }
        return descriptors(actor).isNotEmpty()
    }

    fun descriptors(actor: AuthmanCommandActor): List<AuthmanCommandDescriptor> {
        lock.readLock().lock()
        return try {
            if (!runtimeReady || !coreConnected) {
                emptyList()
            } else {
                provider?.descriptors(actor).orEmpty()
            }
        } catch (ex: Exception) {
            logger.warn("Authman runtime failed to describe its commands", ex)
            emptyList()
        } finally {
            lock.readLock().unlock()
        }
    }

    fun execute(actor: AuthmanCommandActor, arguments: List<String>): Boolean {
        val action = arguments.firstOrNull()?.trim()?.lowercase().orEmpty()
        if (action == "reload") {
            if (!canReload(actor)) {
                actor.send(AuthmanCommandMessage("You do not have permission to reload Authman.", AuthmanCommandTone.ERROR))
                return true
            }
            return try {
                val connected = control.reloadBootstrapConfigAndReconnect()
                actor.send(
                    AuthmanCommandMessage(
                        if (connected) {
                            "Bootstrap configuration reloaded and Core is available."
                        } else {
                            "Bootstrap configuration reloaded, but Core is still unavailable."
                        },
                        if (connected) AuthmanCommandTone.SUCCESS else AuthmanCommandTone.WARNING,
                    ),
                )
                true
            } catch (ex: Exception) {
                logger.warn("Failed to reload Authman bootstrap configuration", ex)
                actor.send(AuthmanCommandMessage("Authman reload failed: ${safeMessage(ex)}", AuthmanCommandTone.ERROR))
                true
            }
        }

        lock.readLock().lock()
        return try {
            val current = provider
            if (!runtimeReady || !coreConnected || current == null) {
                actor.send(
                    AuthmanCommandMessage(
                        "Authman logic is not ready. Reload the bootstrap after checking its Core configuration.",
                        AuthmanCommandTone.WARNING,
                        AuthmanCommandAction(AuthmanCommandActionType.SUGGEST_COMMAND, "/authman reload"),
                    ),
                )
                true
            } else {
                try {
                    if (!current.execute(actor, arguments)) {
                        actor.send(
                            AuthmanCommandMessage(
                                "Unknown Authman command. Run /authman for the commands available to you.",
                                AuthmanCommandTone.ERROR,
                                AuthmanCommandAction(AuthmanCommandActionType.SUGGEST_COMMAND, "/authman"),
                            ),
                        )
                    }
                    true
                } catch (ex: Exception) {
                    logger.warn("Authman runtime command failed actor={} arguments={}", actor.name, arguments, ex)
                    actor.send(AuthmanCommandMessage("Authman command failed: ${safeMessage(ex)}", AuthmanCommandTone.ERROR))
                    true
                }
            }
        } finally {
            lock.readLock().unlock()
        }
    }

    fun suggest(actor: AuthmanCommandActor, arguments: List<String>): List<String> {
        if (arguments.size <= 1) {
            val prefix = arguments.firstOrNull().orEmpty()
            val roots = LinkedHashSet<String>()
            if (canReload(actor)) {
                roots += "reload"
            }
            descriptors(actor).forEach { descriptor ->
                roots += descriptor.name
                roots += descriptor.aliases
            }
            return roots.filter { it.startsWith(prefix, ignoreCase = true) }.take(MAX_SUGGESTIONS)
        }
        if (arguments.firstOrNull().equals("reload", ignoreCase = true)) {
            return emptyList()
        }
        lock.readLock().lock()
        return try {
            if (!runtimeReady || !coreConnected) {
                emptyList()
            } else {
                provider?.suggest(actor, arguments).orEmpty().distinct().take(MAX_SUGGESTIONS)
            }
        } catch (ex: Exception) {
            logger.warn("Authman runtime command suggestions failed", ex)
            emptyList()
        } finally {
            lock.readLock().unlock()
        }
    }

    private fun canReload(actor: AuthmanCommandActor): Boolean =
        actor.kind != AuthmanCommandActorKind.PLAYER ||
            actor.hasPermission(PERMISSION_ALL) ||
            actor.hasPermission(PERMISSION_ADMIN) ||
            actor.hasPermission(PERMISSION_RELOAD)

    private fun notifyChanged() {
        listeners.forEach { listener ->
            runCatching(listener).onFailure { logger.warn("Authman command capability listener failed", it) }
        }
    }

    private fun safeMessage(error: Throwable): String = error.message?.take(300) ?: error.javaClass.simpleName

    private class VelocityCommandActor(
        private val source: CommandSource,
    ) : AuthmanCommandActor {
        private val player = source as? Player

        override val kind: AuthmanCommandActorKind = if (player == null) AuthmanCommandActorKind.CONSOLE else AuthmanCommandActorKind.PLAYER
        override val name: String = player?.username ?: "console"
        override val uniqueId: String? = player?.uniqueId?.toString()

        override fun hasPermission(permission: String): Boolean = player == null || source.hasPermission(permission)

        override fun send(message: AuthmanCommandMessage) {
            val color = when (message.tone) {
                AuthmanCommandTone.INFO -> NamedTextColor.AQUA
                AuthmanCommandTone.SUCCESS -> NamedTextColor.GREEN
                AuthmanCommandTone.WARNING -> NamedTextColor.GOLD
                AuthmanCommandTone.ERROR -> NamedTextColor.RED
                AuthmanCommandTone.MUTED -> NamedTextColor.GRAY
            }
            var body = Component.text(message.text, color)
            message.action?.let { action ->
                body = when (action.type) {
                    AuthmanCommandActionType.OPEN_URL -> body.clickEvent(ClickEvent.openUrl(action.value))
                        .hoverEvent(HoverEvent.showText(Component.text("Open link", NamedTextColor.GRAY)))
                    AuthmanCommandActionType.SUGGEST_COMMAND -> body.clickEvent(ClickEvent.suggestCommand(action.value))
                        .hoverEvent(HoverEvent.showText(Component.text("Insert command", NamedTextColor.GRAY)))
                    AuthmanCommandActionType.RUN_COMMAND -> body.clickEvent(ClickEvent.runCommand(action.value))
                        .hoverEvent(HoverEvent.showText(Component.text("Run command", NamedTextColor.GRAY)))
                }
            }
            source.sendMessage(
                Component.text("Authman ", NamedTextColor.DARK_AQUA, TextDecoration.BOLD)
                    .append(body.decoration(TextDecoration.BOLD, false)),
            )
        }
    }

    companion object {
        const val PERMISSION_ALL = "authman.command.*"
        const val PERMISSION_ADMIN = "authman.command.admin"
        const val PERMISSION_RELOAD = "authman.command.reload"
        private const val MAX_SUGGESTIONS = 50
    }
}
