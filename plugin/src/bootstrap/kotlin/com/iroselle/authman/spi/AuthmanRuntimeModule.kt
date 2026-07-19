package com.iroselle.authman.spi

import com.velocitypowered.api.proxy.ProxyServer
import org.slf4j.Logger
import java.nio.file.Path

/**
 * Frozen bootstrap/runtime ABI. Feature work must not increment this value.
 * Runtime behavior evolves through Core sync payloads, RuntimeTransport, and
 * the Velocity API exposed by AuthmanRuntimeContext.
 */
const val AUTHMAN_RUNTIME_API_VERSION: Int = 2
const val AUTHMAN_RUNTIME_CONTRACT: String = "authman.velocity.runtime.v1"

data class RuntimeHttpResponse(
    val statusCode: Int,
    val body: String,
) {
    val ok: Boolean get() = statusCode in 200..299
}

interface RuntimeTransport {
    fun post(path: String, body: String): RuntimeHttpResponse
}

interface RuntimeControl {
    fun reconnectNow(): Boolean
    fun reloadBootstrapConfigAndReconnect(): Boolean
    fun isCoreAccessRevoked(): Boolean
    fun lockCoreAccess(reason: String)
}

enum class AuthmanCommandActorKind {
    PLAYER,
    CONSOLE,
    BRIDGE_CONSOLE,
}

enum class AuthmanCommandTone {
    INFO,
    SUCCESS,
    WARNING,
    ERROR,
    MUTED,
}

enum class AuthmanCommandActionType {
    OPEN_URL,
    SUGGEST_COMMAND,
    RUN_COMMAND,
}

data class AuthmanCommandAction(
    val type: AuthmanCommandActionType,
    val value: String,
)

data class AuthmanCommandMessage(
    val text: String,
    val tone: AuthmanCommandTone = AuthmanCommandTone.INFO,
    val action: AuthmanCommandAction? = null,
)

data class AuthmanCommandDescriptor(
    val name: String,
    val aliases: List<String> = emptyList(),
    val summary: String,
    val usage: String,
    val permission: String = "",
)

interface AuthmanCommandActor {
    val kind: AuthmanCommandActorKind
    val name: String
    val uniqueId: String?

    fun hasPermission(permission: String): Boolean
    fun send(message: AuthmanCommandMessage)
}

interface AuthmanCommandProvider {
    fun descriptors(actor: AuthmanCommandActor): List<AuthmanCommandDescriptor>
    fun execute(actor: AuthmanCommandActor, arguments: List<String>): Boolean
    fun suggest(actor: AuthmanCommandActor, arguments: List<String>): List<String>
}

interface AuthmanCommandRegistration : AutoCloseable

interface AuthmanCommandRegistry {
    fun install(provider: AuthmanCommandProvider): AuthmanCommandRegistration
}

/**
 * Stable host services available to every Runtime. Do not add constructor
 * parameters or change property types after the bootstrap contract is frozen.
 */
class AuthmanRuntimeContext(
    val server: ProxyServer,
    val logger: Logger,
    val pluginOwner: Any,
    val dataDirectory: Path,
    val transport: RuntimeTransport,
    val control: RuntimeControl,
    val commands: AuthmanCommandRegistry,
)

data class AuthmanRuntimeStatus(
    val onlinePlayers: Int = 0,
    val maxPlayers: Int = 0,
)

/**
 * Stable boundary between the user-installed bootstrap and Core-delivered code.
 * Implementations must be restartable from an exported state snapshot. New
 * product features belong in Runtime and Core, not in this interface.
 */
interface AuthmanRuntimeModule {
    fun start(context: AuthmanRuntimeContext, previousState: ByteArray?)
    fun applySync(payload: String)
    fun handleNodeMessage(payload: String): String?
    fun status(): AuthmanRuntimeStatus
    fun snapshot(): ByteArray?
    fun stop()
}
