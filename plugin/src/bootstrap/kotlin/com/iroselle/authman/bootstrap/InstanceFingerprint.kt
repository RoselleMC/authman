package com.iroselle.authman.bootstrap

import java.nio.file.Files
import java.nio.file.Path
import java.util.UUID

object InstanceFingerprint {
    fun load(dataDirectory: Path): String {
        Files.createDirectories(dataDirectory)
        val path = dataDirectory.resolve("instance.id")
        if (Files.exists(path)) {
            Files.readString(path).trim().takeIf { it.isNotEmpty() }?.let { return it }
        }
        val generated = UUID.randomUUID().toString()
        Files.writeString(path, "$generated\n")
        return generated
    }
}
