package com.iroselle.authman.bootstrap

import com.google.gson.Gson
import com.google.gson.JsonObject
import com.iroselle.authman.spi.AUTHMAN_RUNTIME_API_VERSION
import com.iroselle.authman.spi.AuthmanRuntimeContext
import com.iroselle.authman.spi.AuthmanRuntimeModule
import com.iroselle.authman.spi.AuthmanRuntimeStatus
import com.iroselle.authman.spi.RuntimeControl
import com.velocitypowered.api.proxy.ProxyServer
import org.slf4j.Logger
import java.net.URLClassLoader
import java.nio.file.AtomicMoveNotSupportedException
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.locks.ReentrantReadWriteLock
import java.util.jar.JarFile

class RuntimeManager(
    private val server: ProxyServer,
    private val logger: Logger,
    private val pluginOwner: Any,
    private val dataDirectory: Path,
    private val transport: CoreTransport,
    private val control: RuntimeControl,
    private val commandRouter: RuntimeCommandRouter,
    private val selectCommandMode: (Int?) -> Unit,
) : AutoCloseable {
    private val gson = Gson()
    private val runtimeDirectory = dataDirectory.resolve("runtime")
    private val executor = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "authman-runtime-loader").apply { isDaemon = true }
    }
    private val generation = AtomicLong(0)
    private val stateLock = Any()
    private val runtimeLock = ReentrantReadWriteLock()

    @Volatile
    private var active: LoadedRuntime? = null

    @Volatile
    private var phase: RuntimePhase = RuntimePhase.EMPTY

    @Volatile
    private var target: RuntimeDescriptor? = null

    @Volatile
    private var latestSync: String = ""

    @Volatile
    private var lastError: String = ""

    @Volatile
    private var pendingSHA: String = ""

    @Volatile
    private var rejectedTargetSHA: String = ""

    @Volatile
    private var closed = false

    fun loadCached() {
        Files.createDirectories(runtimeDirectory)
        val descriptorPath = runtimeDirectory.resolve("current.json")
        val syncPath = runtimeDirectory.resolve("last-sync.json")
        if (!Files.isRegularFile(descriptorPath) || !Files.isRegularFile(syncPath)) {
            return
        }
        try {
            val descriptor = gson.fromJson(Files.readString(descriptorPath), RuntimeDescriptor::class.java)
            val sync = Files.readString(syncPath)
            val path = artifactPath(descriptor.sha256)
            verifyArtifact(path, descriptor)
            latestSync = sync
            activate(descriptor, path, null)
            logger.info("Loaded cached Authman runtime {} ({})", descriptor.version, shortHash(descriptor.sha256))
        } catch (ex: Throwable) {
            throwIfFatal(ex)
            phase = RuntimePhase.FAILED
            lastError = "cached runtime failed: ${safeMessage(ex)}"
            logger.warn("Failed to load cached Authman runtime; new players remain blocked", ex)
        }
    }

    fun applySync(payload: String) {
        if (closed) {
            return
        }
        latestSync = payload
        runCatching {
            withRuntimeRead {
                if (phase == RuntimePhase.READY) {
                    active?.module?.applySync(payload)
                }
            }
        }
            .onFailure { ex ->
                lastError = "runtime sync failed: ${safeMessage(ex)}"
                logger.warn("Authman runtime rejected a Core sync payload", ex)
        }
        val descriptor = parseDescriptor(payload) ?: return
        val previousTargetSHA = target?.sha256.orEmpty()
        val targetChanged = !previousTargetSHA.equals(descriptor.sha256, ignoreCase = true)
        target = descriptor
        if (targetChanged) {
            rejectedTargetSHA = ""
        }
        if (active?.descriptor?.sha256.equals(descriptor.sha256, ignoreCase = true)) {
            if (targetChanged && phase == RuntimePhase.READY) {
                lastError = ""
                runCatching { control.reconnectNow() }
            }
            return
        }
        if (rejectedTargetSHA.equals(descriptor.sha256, ignoreCase = true)) {
            return
        }
        synchronized(stateLock) {
            if (pendingSHA.equals(descriptor.sha256, ignoreCase = true)) {
                return
            }
            pendingSHA = descriptor.sha256
        }
        executor.execute {
            try {
                if (isCurrentTarget(descriptor)) {
                    installTarget(descriptor)
                }
            } catch (ex: Throwable) {
                throwIfFatal(ex)
                rejectedTargetSHA = descriptor.sha256
                lastError = "runtime update failed: ${safeMessage(ex)}"
                if (active == null) {
                    phase = RuntimePhase.FAILED
                }
                logger.warn("Failed to install Authman runtime {} ({})", descriptor.version, shortHash(descriptor.sha256), ex)
            } finally {
                synchronized(stateLock) {
                    if (pendingSHA.equals(descriptor.sha256, ignoreCase = true)) {
                        pendingSHA = ""
                    }
                }
            }
        }
    }

    fun handleNodeMessage(payload: String): String? {
        if (!isReady()) {
            return null
        }
        return try {
            withRuntimeRead {
                if (phase == RuntimePhase.READY) active?.module?.handleNodeMessage(payload) else null
            }
        } catch (ex: Exception) {
            lastError = "runtime message failed: ${safeMessage(ex)}"
            logger.warn("Authman runtime failed to handle a node message", ex)
            null
        }
    }

    fun isReady(): Boolean = phase == RuntimePhase.READY && active != null

    fun admissionGeneration(): Long? = if (isReady() && !control.isCoreAccessRevoked()) generation.get() else null

    fun activeApiVersion(): Int? = active?.descriptor?.apiVersion

    fun runtimeStatus(): AuthmanRuntimeStatus = runCatching {
        withRuntimeRead { active?.module?.status() }
    }
        .getOrNull() ?: AuthmanRuntimeStatus(onlinePlayers = server.playerCount, maxPlayers = server.configuration.showMaxPlayers)

    fun report(): Map<String, Any?> {
        val current = active?.descriptor
        return mapOf(
            "state" to phase.wireName,
            "api_version" to (current?.apiVersion ?: AUTHMAN_RUNTIME_API_VERSION),
            "version" to (current?.version ?: ""),
            "sha256" to (current?.sha256 ?: ""),
            "target_version" to (target?.version ?: ""),
            "target_sha256" to (target?.sha256 ?: ""),
            "last_error" to lastError,
        )
    }

    override fun close() {
        closed = true
        commandRouter.suspendRuntime()
        phase = RuntimePhase.STOPPING
        executor.shutdownNow()
        runCatching { executor.awaitTermination(5, TimeUnit.SECONDS) }
        withRuntimeWrite {
            val previous = active
            active = null
            stopRuntime(previous, strict = false)
            phase = RuntimePhase.EMPTY
        }
    }

    private fun installTarget(descriptor: RuntimeDescriptor) {
        val path = artifactPath(descriptor.sha256)
        if (!Files.isRegularFile(path)) {
            val bytes = transport.download(descriptor.downloadPath, MAX_ARTIFACT_BYTES)
            require(bytes.size.toLong() == descriptor.sizeBytes) {
                "runtime size mismatch: expected ${descriptor.sizeBytes}, got ${bytes.size}"
            }
            require(sha256(bytes).equals(descriptor.sha256, ignoreCase = true)) { "runtime SHA-256 mismatch" }
            val temp = Files.createTempFile(runtimeDirectory, "runtime-", ".jar.tmp")
            try {
                Files.write(temp, bytes)
                try {
                    Files.move(temp, path, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
                } catch (_: AtomicMoveNotSupportedException) {
                    Files.move(temp, path, StandardCopyOption.REPLACE_EXISTING)
                }
            } finally {
                Files.deleteIfExists(temp)
            }
        }
        verifyArtifact(path, descriptor)
        if (closed || !isCurrentTarget(descriptor)) {
            logger.info("Discarding stale Authman runtime target {} ({})", descriptor.version, shortHash(descriptor.sha256))
            return
        }
        activate(descriptor, path, active)
    }

    private fun activate(descriptor: RuntimeDescriptor, path: Path, previous: LoadedRuntime?) {
        withRuntimeWrite {
            activateLocked(descriptor, path, previous)
        }
        runCatching { control.reconnectNow() }
            .onFailure { logger.warn("Failed to refresh Core after an Authman runtime transition", it) }
    }

    private fun activateLocked(descriptor: RuntimeDescriptor, path: Path, previous: LoadedRuntime?) {
        commandRouter.suspendRuntime()
        phase = RuntimePhase.QUIESCING
        val previousState = try {
            previous?.module?.snapshot()
        } catch (ex: Throwable) {
            throwIfFatal(ex)
            phase = RuntimePhase.READY
            commandRouter.resumeRuntime()
            selectCommandMode(previous?.descriptor?.apiVersion)
            throw IllegalStateException("current runtime state could not be exported; update aborted", ex)
        }

        if (previous != null) {
            val stopError = stopRuntime(previous, strict = true)
            if (stopError != null) {
                active = previous
                phase = RuntimePhase.FAILED
                selectCommandMode(previous.descriptor.apiVersion)
                lastError = "current runtime could not stop cleanly: ${safeMessage(stopError)}"
                throw IllegalStateException(lastError, stopError)
            }
            active = null
        }

        phase = RuntimePhase.ACTIVATING
        var candidate: LoadedRuntime? = null
        try {
            selectCommandMode(descriptor.apiVersion)
            candidate = loadRuntime(descriptor, path, previousState)
            val sync = latestSync
            if (sync.isNotBlank()) {
                candidate.module.applySync(sync)
            }
            active = candidate
            generation.incrementAndGet()
            phase = RuntimePhase.READY
            commandRouter.resumeRuntime()
            rejectedTargetSHA = ""
            lastError = ""
            persistCurrentSafely(descriptor, sync)
            cleanupArtifactsSafely(descriptor.sha256, previous?.descriptor?.sha256)
            logger.info("Activated Authman runtime {} ({})", descriptor.version, shortHash(descriptor.sha256))
        } catch (activationError: Throwable) {
            throwIfFatal(activationError)
            stopRuntime(candidate, strict = false)
            active = null
            if (previous == null) {
                phase = RuntimePhase.FAILED
                selectCommandMode(null)
                throw activationError
            }
            logger.warn("Authman runtime activation failed; rolling back to {}", previous.descriptor.version, activationError)
            try {
                selectCommandMode(previous.descriptor.apiVersion)
                val rollback = loadRuntime(previous.descriptor, artifactPath(previous.descriptor.sha256), previousState)
                val sync = latestSync
                if (sync.isNotBlank()) {
                    rollback.module.applySync(sync)
                }
                active = rollback
                generation.incrementAndGet()
                phase = RuntimePhase.READY
                commandRouter.resumeRuntime()
                rejectedTargetSHA = descriptor.sha256
                lastError = "runtime ${descriptor.version} failed; rolled back: ${safeMessage(activationError)}"
                persistCurrentSafely(previous.descriptor, sync)
                logger.info("Rolled back to Authman runtime {} ({})", previous.descriptor.version, shortHash(previous.descriptor.sha256))
            } catch (rollbackError: Throwable) {
                throwIfFatal(rollbackError)
                active = null
                phase = RuntimePhase.FAILED
                selectCommandMode(null)
                lastError = "runtime activation and rollback failed: ${safeMessage(rollbackError)}"
                activationError.addSuppressed(rollbackError)
                throw activationError
            }
        }
    }

    private fun loadRuntime(descriptor: RuntimeDescriptor, path: Path, state: ByteArray?): LoadedRuntime {
        verifyArtifact(path, descriptor)
        val loader = URLClassLoader(arrayOf(path.toUri().toURL()), javaClass.classLoader)
        var module: AuthmanRuntimeModule? = null
        try {
            val type = Class.forName(descriptor.entrypoint, true, loader)
            require(AuthmanRuntimeModule::class.java.isAssignableFrom(type)) {
                "runtime entrypoint does not implement AuthmanRuntimeModule"
            }
            module = type.getDeclaredConstructor().newInstance() as AuthmanRuntimeModule
            module.start(
                AuthmanRuntimeContext(
                    server = server,
                    logger = logger,
                    pluginOwner = pluginOwner,
                    dataDirectory = dataDirectory,
                    transport = transport,
                    control = control,
                    commands = commandRouter,
                ),
                state,
            )
            return LoadedRuntime(descriptor, module, loader)
        } catch (ex: Throwable) {
            runCatching { module?.stop() }
            runCatching { loader.close() }
            throwIfFatal(ex)
            throw ex
        }
    }

    private fun stopRuntime(runtime: LoadedRuntime?, strict: Boolean): Throwable? {
        if (runtime == null) {
            return null
        }
        val stopError = runCatching { runtime.module.stop() }.exceptionOrNull()
        if (stopError != null) {
            logger.warn("Failed to stop Authman runtime {} cleanly", runtime.descriptor.version, stopError)
            if (strict) {
                return stopError
            }
        }
        runCatching { runtime.loader.close() }
            .onFailure { logger.warn("Failed to close Authman runtime classloader", it) }
        return stopError
    }

    private fun verifyArtifact(path: Path, descriptor: RuntimeDescriptor) {
        require(Files.isRegularFile(path)) { "runtime artifact is missing" }
        val size = Files.size(path)
        require(size == descriptor.sizeBytes) { "runtime artifact size mismatch" }
        require(size in 1..MAX_ARTIFACT_BYTES) { "runtime artifact size is invalid" }
        require(sha256(Files.readAllBytes(path)).equals(descriptor.sha256, ignoreCase = true)) {
            "runtime artifact SHA-256 mismatch"
        }
        JarFile(path.toFile(), true).use { jar ->
            val attributes = requireNotNull(jar.manifest) { "runtime manifest is missing" }.mainAttributes
            require(attributes.getValue(MANIFEST_ENTRYPOINT) == descriptor.entrypoint) { "runtime entrypoint manifest mismatch" }
            require(attributes.getValue(MANIFEST_VERSION) == descriptor.version) { "runtime version manifest mismatch" }
            require(attributes.getValue(MANIFEST_API)?.toIntOrNull() == descriptor.apiVersion) { "runtime API manifest mismatch" }
            RuntimeCompatibility.requireCompatible(descriptor.apiVersion, attributes.getValue(MANIFEST_CONTRACT)?.trim())
        }
    }

    private fun parseDescriptor(payload: String): RuntimeDescriptor? {
        val root = gson.fromJson(payload, JsonObject::class.java)
        val module = root.getAsJsonObject("runtime_module") ?: return null
        val configured = module.getAsJsonObject("configured") ?: return null
        val sha = configured.string("sha256").lowercase()
        if (sha.length != 64 || sha.any { it !in '0'..'9' && it !in 'a'..'f' }) {
            throw IllegalArgumentException("Core returned an invalid runtime SHA-256")
        }
        return RuntimeDescriptor(
            id = configured.string("id"),
            version = configured.string("version"),
            apiVersion = configured.int("api_version"),
            entrypoint = configured.string("entrypoint"),
            sizeBytes = configured.long("size_bytes"),
            sha256 = sha,
            downloadPath = module.string("download_path"),
        )
    }

    private fun persistCurrent(descriptor: RuntimeDescriptor, sync: String) {
        Files.createDirectories(runtimeDirectory)
        atomicWrite(runtimeDirectory.resolve("current.json"), gson.toJson(descriptor))
        if (sync.isNotBlank()) {
            atomicWrite(runtimeDirectory.resolve("last-sync.json"), sync)
        }
    }

    private fun atomicWrite(path: Path, value: String) {
        val temp = Files.createTempFile(runtimeDirectory, path.fileName.toString(), ".tmp")
        try {
            Files.writeString(temp, value)
            try {
                Files.move(temp, path, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            } catch (_: AtomicMoveNotSupportedException) {
                Files.move(temp, path, StandardCopyOption.REPLACE_EXISTING)
            }
        } finally {
            Files.deleteIfExists(temp)
        }
    }

    private fun persistCurrentSafely(descriptor: RuntimeDescriptor, sync: String) {
        runCatching { persistCurrent(descriptor, sync) }
            .onFailure { ex ->
                lastError = "runtime is active but its cache metadata could not be persisted: ${safeMessage(ex)}"
                logger.warn("Authman runtime activated, but its cache metadata could not be persisted", ex)
            }
    }

    private fun cleanupArtifactsSafely(currentSHA: String, previousSHA: String?) {
        runCatching { cleanupArtifacts(currentSHA, previousSHA) }
            .onFailure { logger.warn("Failed to clean old Authman runtime artifacts", it) }
    }

    private fun cleanupArtifacts(currentSHA: String, previousSHA: String?) {
        Files.list(runtimeDirectory).use { files ->
            files.filter { it.fileName.toString().endsWith(".jar") }
                .filter { path ->
                    val name = path.fileName.toString().removeSuffix(".jar")
                    !name.equals(currentSHA, true) && !name.equals(previousSHA, true)
                }
                .forEach { runCatching { Files.deleteIfExists(it) } }
        }
    }

    private fun artifactPath(sha: String): Path = runtimeDirectory.resolve("${sha.lowercase()}.jar")

    private fun isCurrentTarget(descriptor: RuntimeDescriptor): Boolean =
        target?.sha256.equals(descriptor.sha256, ignoreCase = true)

    private inline fun <T> withRuntimeRead(block: () -> T): T {
        runtimeLock.readLock().lock()
        return try {
            block()
        } finally {
            runtimeLock.readLock().unlock()
        }
    }

    private inline fun <T> withRuntimeWrite(block: () -> T): T {
        runtimeLock.writeLock().lock()
        return try {
            block()
        } finally {
            runtimeLock.writeLock().unlock()
        }
    }

    private fun sha256(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256")
        .digest(bytes)
        .joinToString("") { "%02x".format(it) }

    private fun safeMessage(error: Throwable): String = error.message?.take(500) ?: error.javaClass.simpleName

    private fun throwIfFatal(error: Throwable) {
        if (error is VirtualMachineError) {
            throw error
        }
    }

    private fun shortHash(value: String): String = value.take(12)

    private data class LoadedRuntime(
        val descriptor: RuntimeDescriptor,
        val module: AuthmanRuntimeModule,
        val loader: URLClassLoader,
    )

    data class RuntimeDescriptor(
        val id: String,
        val version: String,
        val apiVersion: Int,
        val entrypoint: String,
        val sizeBytes: Long,
        val sha256: String,
        val downloadPath: String,
    )

    private enum class RuntimePhase(val wireName: String) {
        EMPTY("not_loaded"),
        QUIESCING("quiescing"),
        ACTIVATING("activating"),
        READY("ready"),
        FAILED("failed"),
        STOPPING("stopping"),
    }

    companion object {
        private const val MAX_ARTIFACT_BYTES = 64L * 1024L * 1024L
        private const val MANIFEST_ENTRYPOINT = "Authman-Runtime-Entrypoint"
        private const val MANIFEST_VERSION = "Authman-Runtime-Version"
        private const val MANIFEST_API = "Authman-Runtime-Api"
        private const val MANIFEST_CONTRACT = "Authman-Runtime-Contract"
    }
}

private fun JsonObject.string(key: String): String =
    get(key)?.takeIf { !it.isJsonNull }?.asString?.trim().orEmpty().also {
        require(it.isNotEmpty()) { "Core runtime descriptor is missing $key" }
    }

private fun JsonObject.int(key: String): Int =
    get(key)?.takeIf { !it.isJsonNull }?.asInt ?: error("Core runtime descriptor is missing $key")

private fun JsonObject.long(key: String): Long =
    get(key)?.takeIf { !it.isJsonNull }?.asLong ?: error("Core runtime descriptor is missing $key")
