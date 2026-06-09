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

    val serverId: String
        get() = runtime.serverId

    val heartbeatIntervalSeconds: Long
        get() = runtime.heartbeatIntervalSeconds.coerceAtLeast(10)

    val requestTimeout: Duration
        get() = Duration.ofSeconds(api.requestTimeoutSeconds.coerceAtLeast(1))

    val transferCookieKey: String
        get() = runtime.transferCookieKey

    val downstreamInitialServer: String
        get() = runtime.downstreamInitialServer

    val downstreamHoldingServer: String
        get() = runtime.downstreamHoldingServer

    val downstreamValidationTimeoutSeconds: Long
        get() = runtime.downstreamValidationTimeoutSeconds.coerceAtLeast(3)

    val runtime: RuntimeConfig
        get() = runtimeRef.get()

    fun applyRuntime(next: RuntimeConfig) {
        runtimeRef.set(next.normalized())
    }

    fun replaceLocal(next: AuthmanConfig) {
        api = next.api
        runtimeRef.set(next.runtime.normalized())
    }

    fun validate(configPath: Path) {
        require(api.baseUrl.isNotBlank()) { "api.base-url must be configured in $configPath" }
        require(nodeToken.isNotEmpty()) { "api.node-token must be configured in $configPath" }
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
    val transferCookieKey: String = "authman:transfer_grant",
    val downstreamInitialServer: String = "",
    val downstreamHoldingServer: String = "",
    val downstreamValidationTimeoutSeconds: Long = 10,
) {
    fun normalized(): RuntimeConfig {
        return copy(
            nodeName = nodeName.trim().ifEmpty { "downstream" },
            serverId = serverId.trim().ifEmpty { "default" },
            heartbeatIntervalSeconds = heartbeatIntervalSeconds.coerceAtLeast(10),
            transferCookieKey = transferCookieKey.trim().ifEmpty { "authman:transfer_grant" },
            downstreamValidationTimeoutSeconds = downstreamValidationTimeoutSeconds.coerceAtLeast(3),
        )
    }
}
