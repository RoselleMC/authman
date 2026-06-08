package mc.roselle.authman.config

import org.spongepowered.configurate.objectmapping.ConfigSerializable
import org.spongepowered.configurate.objectmapping.meta.Setting
import org.spongepowered.configurate.yaml.YamlConfigurationLoader
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.time.Duration
import java.util.concurrent.atomic.AtomicReference

@ConfigSerializable
class AuthmanConfig {
    @field:Setting("mode")
    var mode: String = "portal"

    @field:Setting("api")
    var api: ApiConfig = ApiConfig()

    @Transient
    private val runtimeRef = AtomicReference(RuntimeConfig())

    val apiBase: URI
        get() = URI.create(api.baseUrl.trim())

    val nodeToken: String
        get() = api.nodeToken.trim()

    val nodeName: String
        get() = runtime.nodeName

    val runtimeMode: RuntimeMode
        get() = RuntimeMode.from(mode)

    val serverId: String
        get() = runtime.serverId

    val heartbeatIntervalSeconds: Long
        get() = runtime.heartbeatIntervalSeconds.coerceAtLeast(10)

    val requestTimeout: Duration
        get() = Duration.ofSeconds(api.requestTimeoutSeconds.coerceAtLeast(1))

    val resolveRawOfflineNames: Boolean
        get() = runtime.resolveRawOfflineNames

    val maxPasswordAttempts: Int
        get() = runtime.maxPasswordAttempts.coerceAtLeast(1)

    val chatCooldownMillis: Long
        get() = runtime.chatCooldownMillis.coerceAtLeast(0)

    val authTimeoutSeconds: Long
        get() = runtime.authTimeoutSeconds.coerceAtLeast(10)

    val completionDelaySeconds: Long
        get() = runtime.completionDelaySeconds.coerceAtLeast(0)

    val defaultTargetServer: String
        get() = runtime.defaultTargetServer

    val holdingServer: String
        get() = runtime.holdingServer

    val transferCookieKey: String
        get() = runtime.transferCookieKey

    val gateInitialServer: String
        get() = runtime.gateInitialServer

    val gateHoldingServer: String
        get() = runtime.gateHoldingServer

    val gateValidationTimeoutSeconds: Long
        get() = runtime.gateValidationTimeoutSeconds.coerceAtLeast(3)

    val portalRequestedServerId: String
        get() = runtime.portalRequestedServerId

    val portalRequestedHost: String
        get() = runtime.portalRequestedHost

    val portalSourceId: String
        get() = runtime.portalSourceId.ifEmpty { nodeName }

    val dialogEnabled: Boolean
        get() = runtime.dialogEnabled

    val dialogFallbackChatEnabled: Boolean
        get() = runtime.dialogFallbackChatEnabled

    val emailVerificationMode: String
        get() = runtime.emailVerificationMode

    val runtime: RuntimeConfig
        get() = runtimeRef.get()

    fun applyRuntime(next: RuntimeConfig) {
        runtimeRef.set(next.normalized(runtimeMode))
    }

    fun validate(configPath: Path) {
        require(api.baseUrl.isNotBlank()) { "api.base-url must be configured in $configPath" }
        require(nodeToken.isNotEmpty()) { "api.node-token must be configured in $configPath" }
        RuntimeMode.from(mode)
    }

    companion object {
        private const val CONFIG_FILE_NAME = "config.yml"
        private const val CONFIG_RESOURCE = "/config.yml"

        fun load(dataDirectory: Path): AuthmanConfig {
            Files.createDirectories(dataDirectory)
            val configPath = dataDirectory.resolve(CONFIG_FILE_NAME)
            if (!Files.exists(configPath)) {
                copyBundledDefaultConfig(configPath)
            }
            val loader = YamlConfigurationLoader.builder()
                .path(configPath)
                .build()
            val config = loader.load().get(AuthmanConfig::class.java)
                ?: error("failed to load $configPath")
            config.validate(configPath)
            return config
        }

        private fun copyBundledDefaultConfig(configPath: Path) {
            val stream = AuthmanConfig::class.java.getResourceAsStream(CONFIG_RESOURCE)
                ?: error("bundled $CONFIG_FILE_NAME resource is missing")
            stream.use {
                Files.copy(it, configPath, StandardCopyOption.REPLACE_EXISTING)
            }
        }
    }
}

enum class RuntimeMode {
    PORTAL,
    GATE;

    companion object {
        fun from(value: String): RuntimeMode {
            return when (value.trim().lowercase()) {
                "", "portal" -> PORTAL
                "gate" -> GATE
                else -> error("mode must be portal or gate")
            }
        }
    }
}

@ConfigSerializable
class ApiConfig {
    @field:Setting("base-url")
    var baseUrl: String = ""

    @field:Setting("node-token")
    var nodeToken: String = ""

    @field:Setting("request-timeout-seconds")
    var requestTimeoutSeconds: Long = 8
}

data class RuntimeConfig(
    val nodeName: String = "velocity",
    val serverId: String = "default",
    val heartbeatIntervalSeconds: Long = 60,
    val resolveRawOfflineNames: Boolean = true,
    val maxPasswordAttempts: Int = 3,
    val chatCooldownMillis: Long = 150,
    val authTimeoutSeconds: Long = 90,
    val completionDelaySeconds: Long = 3,
    val defaultTargetServer: String = "",
    val holdingServer: String = "",
    val transferCookieKey: String = "authman:transfer_grant",
    val gateInitialServer: String = "",
    val gateHoldingServer: String = "",
    val gateValidationTimeoutSeconds: Long = 10,
    val portalRequestedServerId: String = "",
    val portalRequestedHost: String = "",
    val portalSourceId: String = "",
    val dialogEnabled: Boolean = true,
    val dialogFallbackChatEnabled: Boolean = true,
    val emailVerificationMode: String = "disabled",
) {
    fun normalized(mode: RuntimeMode): RuntimeConfig {
        val fallbackName = if (mode == RuntimeMode.PORTAL) "portal" else "gate"
        return copy(
            nodeName = nodeName.trim().ifEmpty { fallbackName },
            serverId = serverId.trim().ifEmpty { "default" },
            heartbeatIntervalSeconds = heartbeatIntervalSeconds.coerceAtLeast(10),
            maxPasswordAttempts = maxPasswordAttempts.coerceAtLeast(1),
            chatCooldownMillis = chatCooldownMillis.coerceAtLeast(0),
            authTimeoutSeconds = authTimeoutSeconds.coerceAtLeast(10),
            completionDelaySeconds = completionDelaySeconds.coerceAtLeast(0),
            transferCookieKey = transferCookieKey.trim().ifEmpty { "authman:transfer_grant" },
            gateValidationTimeoutSeconds = gateValidationTimeoutSeconds.coerceAtLeast(3),
            emailVerificationMode = emailVerificationMode.trim().lowercase().ifEmpty { "disabled" },
        )
    }
}
