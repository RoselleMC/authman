package mc.roselle.authman.config

import java.nio.file.Files
import java.nio.file.Path
import java.util.UUID

object InstanceFingerprint {
    fun load(dataDirectory: Path): String {
        Files.createDirectories(dataDirectory)
        val path = dataDirectory.resolve("instance.id")
        if (Files.exists(path)) {
            val existing = Files.readString(path).trim()
            if (existing.isNotEmpty()) {
                return existing
            }
        }
        val generated = UUID.randomUUID().toString()
        Files.writeString(path, "$generated\n")
        return generated
    }
}
