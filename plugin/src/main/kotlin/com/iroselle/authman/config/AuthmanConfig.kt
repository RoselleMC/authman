package com.iroselle.authman.config

import java.util.concurrent.atomic.AtomicReference

/** Runtime-only configuration delivered by Core through bootstrap sync. */
class AuthmanConfig {
    private val runtimeRef = AtomicReference(RuntimeConfig())

    val nodeName: String get() = runtime.nodeName
    val serverId: String get() = runtime.serverId
    val heartbeatIntervalSeconds: Long get() = runtime.heartbeatIntervalSeconds.coerceAtLeast(10)
    val transferCookieKey: String get() = runtime.transferCookieKey
    val downstreamInitialServer: String get() = runtime.downstreamInitialServer
    val downstreamHoldingServer: String get() = runtime.downstreamHoldingServer
    val downstreamValidationTimeoutSeconds: Long get() = runtime.downstreamValidationTimeoutSeconds.coerceAtLeast(3)
    val runtime: RuntimeConfig get() = runtimeRef.get()

    fun applyRuntime(next: RuntimeConfig) {
        runtimeRef.set(next.normalized())
    }
}

data class RuntimeConfig(
    val nodeName: String = "velocity",
    val serverId: String = "default",
    val heartbeatIntervalSeconds: Long = 60,
    val transferCookieKey: String = "authman:transfer_grant",
    val downstreamInitialServer: String = "",
    val downstreamHoldingServer: String = "",
    val downstreamValidationTimeoutSeconds: Long = 10,
    val websocketEnabled: Boolean = true,
    val websocketReconnectMinSeconds: Long = 2,
    val websocketReconnectMaxSeconds: Long = 60,
    val websocketPingIntervalSeconds: Long = 25,
) {
    fun normalized(): RuntimeConfig {
        val reconnectMin = websocketReconnectMinSeconds.coerceIn(1, 300)
        return copy(
            nodeName = nodeName.trim().ifEmpty { "downstream" },
            serverId = serverId.trim().ifEmpty { "default" },
            heartbeatIntervalSeconds = heartbeatIntervalSeconds.coerceAtLeast(10),
            transferCookieKey = transferCookieKey.trim().ifEmpty { "authman:transfer_grant" },
            downstreamValidationTimeoutSeconds = downstreamValidationTimeoutSeconds.coerceAtLeast(3),
            websocketReconnectMinSeconds = reconnectMin,
            websocketReconnectMaxSeconds = websocketReconnectMaxSeconds.coerceAtLeast(reconnectMin),
            websocketPingIntervalSeconds = websocketPingIntervalSeconds.coerceAtLeast(5),
        )
    }
}
