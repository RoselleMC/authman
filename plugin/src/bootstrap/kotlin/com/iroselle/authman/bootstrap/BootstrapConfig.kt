package com.iroselle.authman.bootstrap

import org.spongepowered.configurate.objectmapping.ConfigSerializable
import org.spongepowered.configurate.objectmapping.meta.Setting
import org.spongepowered.configurate.yaml.YamlConfigurationLoader
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.time.Duration

@ConfigSerializable
class BootstrapConfig {
    @field:Setting("api")
    var api: BootstrapApiConfig = BootstrapApiConfig()

    val apiBase: URI get() = URI.create(api.baseUrl.trim())
    val nodeToken: String get() = api.nodeToken.trim()
    val requestTimeout: Duration get() = Duration.ofSeconds(api.requestTimeoutSeconds.coerceIn(1, 60))

    fun validate(path: Path) {
        require(api.baseUrl.isNotBlank()) { "api.base-url must be configured in $path" }
        require(nodeToken.isNotBlank()) { "api.node-token must be configured in $path" }
    }

    companion object {
        fun load(dataDirectory: Path): BootstrapConfig {
            Files.createDirectories(dataDirectory)
            val path = dataDirectory.resolve("config.yml")
            if (!Files.exists(path)) {
                BootstrapConfig::class.java.getResourceAsStream("/config.yml").use { stream ->
                    requireNotNull(stream) { "bundled config.yml resource is missing" }
                    Files.copy(stream, path, StandardCopyOption.REPLACE_EXISTING)
                }
            }
            val config = YamlConfigurationLoader.builder().path(path).build().load().get(BootstrapConfig::class.java)
                ?: error("failed to load $path")
            config.validate(path)
            return config
        }
    }
}

@ConfigSerializable
class BootstrapApiConfig {
    @field:Setting("base-url")
    var baseUrl: String = ""

    @field:Setting("node-token")
    var nodeToken: String = ""

    @field:Setting("request-timeout-seconds")
    var requestTimeoutSeconds: Long = 8
}
