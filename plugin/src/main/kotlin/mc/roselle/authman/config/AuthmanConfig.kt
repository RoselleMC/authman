package mc.roselle.authman.config

import org.spongepowered.configurate.objectmapping.ConfigSerializable
import org.spongepowered.configurate.objectmapping.meta.Setting
import org.spongepowered.configurate.yaml.YamlConfigurationLoader
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.time.Duration

@ConfigSerializable
class AuthmanConfig {
    @field:Setting("mode")
    var mode: String = "portal"

    @field:Setting("api")
    var api: ApiConfig = ApiConfig()

    @field:Setting("node")
    var node: NodeConfig = NodeConfig()

    @field:Setting("identity")
    var identity: IdentityConfig = IdentityConfig()

    @field:Setting("auth")
    var auth: AuthConfig = AuthConfig()

    @field:Setting("servers")
    var servers: ServersConfig = ServersConfig()

    @field:Setting("portal")
    var portal: PortalConfig = PortalConfig()

    @field:Setting("gate")
    var gate: GateConfig = GateConfig()

    @field:Setting("dialog")
    var dialog: DialogConfig = DialogConfig()

    @field:Setting("email")
    var email: EmailConfig = EmailConfig()

    @field:Setting("packets")
    var packets: PacketsConfig = PacketsConfig()

    val apiBase: URI
        get() = URI.create(api.baseUrl.trim())

    val nodeToken: String
        get() = api.nodeToken.trim()

    val nodeName: String
        get() = node.name.trim().ifEmpty { "velocity" }

    val runtimeMode: RuntimeMode
        get() = RuntimeMode.from(mode)

    val serverId: String
        get() = node.serverId.trim().ifEmpty { "default" }

    val heartbeatIntervalSeconds: Long
        get() = node.heartbeatIntervalSeconds.coerceAtLeast(10)

    val requestTimeout: Duration
        get() = Duration.ofSeconds(api.requestTimeoutSeconds.coerceAtLeast(1))

    val resolveRawOfflineNames: Boolean
        get() = identity.resolveRawOfflineNames

    val maxPasswordAttempts: Int
        get() = auth.maxPasswordAttempts.coerceAtLeast(1)

    val chatCooldownMillis: Long
        get() = auth.chatCooldownMillis.coerceAtLeast(0)

    val authTimeoutSeconds: Long
        get() = auth.timeoutSeconds.coerceAtLeast(10)

    val completionDelaySeconds: Long
        get() = auth.completionDelaySeconds.coerceAtLeast(0)

    val defaultTargetServer: String
        get() = servers.defaultTarget.trim()

    val holdingServer: String
        get() = servers.holding.trim()

    val transferCookieKey: String
        get() = gate.transferCookieKey.trim().ifEmpty { "authman:transfer_grant" }

    val gateInitialServer: String
        get() = gate.initialServer.trim()

    val gateHoldingServer: String
        get() = gate.holdingServer.trim()

    val gateValidationTimeoutSeconds: Long
        get() = gate.validationTimeoutSeconds.coerceAtLeast(3)

    val portalRequestedServerId: String
        get() = portal.serverId.trim()

    val portalRequestedHost: String
        get() = portal.requestedHost.trim()

    val portalSourceId: String
        get() = portal.sourceId.trim().ifEmpty { nodeName }

    val dialogEnabled: Boolean
        get() = dialog.enabled

    val dialogFallbackChatEnabled: Boolean
        get() = dialog.fallbackChat

    val emailVerificationMode: String
        get() = email.verificationMode.trim().lowercase()

    val stripOfflinePrefix: StripOfflinePrefixConfig
        get() = packets.stripOfflinePrefix

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
    var requestTimeoutSeconds: Long = 0
}

@ConfigSerializable
class NodeConfig {
    @field:Setting("name")
    var name: String = ""

    @field:Setting("server-id")
    var serverId: String = ""

    @field:Setting("heartbeat-interval-seconds")
    var heartbeatIntervalSeconds: Long = 0
}

@ConfigSerializable
class IdentityConfig {
    @field:Setting("resolve-raw-offline-names")
    var resolveRawOfflineNames: Boolean = false
}

@ConfigSerializable
class AuthConfig {
    @field:Setting("max-password-attempts")
    var maxPasswordAttempts: Int = 0

    @field:Setting("chat-cooldown-millis")
    var chatCooldownMillis: Long = 0

    @field:Setting("timeout-seconds")
    var timeoutSeconds: Long = 0

    @field:Setting("completion-delay-seconds")
    var completionDelaySeconds: Long = 0
}

@ConfigSerializable
class ServersConfig {
    @field:Setting("default-target")
    var defaultTarget: String = ""

    @field:Setting("holding")
    var holding: String = ""
}

@ConfigSerializable
class PortalConfig {
    @field:Setting("server-id")
    var serverId: String = ""

    @field:Setting("requested-host")
    var requestedHost: String = ""

    @field:Setting("source-id")
    var sourceId: String = ""
}

@ConfigSerializable
class GateConfig {
    @field:Setting("initial-server")
    var initialServer: String = ""

    @field:Setting("holding-server")
    var holdingServer: String = ""

    @field:Setting("transfer-cookie-key")
    var transferCookieKey: String = "authman:transfer_grant"

    @field:Setting("validation-timeout-seconds")
    var validationTimeoutSeconds: Long = 10
}

@ConfigSerializable
class DialogConfig {
    @field:Setting("enabled")
    var enabled: Boolean = true

    @field:Setting("fallback-chat")
    var fallbackChat: Boolean = true
}

@ConfigSerializable
class EmailConfig {
    @field:Setting("verification-mode")
    var verificationMode: String = ""
}

@ConfigSerializable
class PacketsConfig {
    @field:Setting("strip-offline-prefix")
    var stripOfflinePrefix: StripOfflinePrefixConfig = StripOfflinePrefixConfig()
}

@ConfigSerializable
class StripOfflinePrefixConfig {
    @field:Setting("enabled")
    var enabled: Boolean = false

    @field:Setting("player-info-packets")
    var playerInfoPackets: Boolean = false

    @field:Setting("scoreboard-team-packets")
    var scoreboardTeamPackets: Boolean = false

    @field:Setting("strip-when-premium-name-exists")
    var stripWhenPremiumNameExists: Boolean = false
}
