plugins {
    kotlin("jvm") version "2.0.21"
    id("com.gradleup.shadow") version "8.3.6"
}

group = "com.iroselle.authman"
version = "0.1.0-dev"

dependencies {
    compileOnly("com.velocitypowered:velocity-api:3.5.0-SNAPSHOT")
    compileOnly("com.github.retrooper:packetevents-velocity:2.12.1")

    implementation("com.google.code.gson:gson:2.11.0")
    implementation("org.spongepowered:configurate-yaml:4.2.0")
}

kotlin {
    jvmToolchain(21)
}

tasks {
    jar {
        archiveBaseName.set("authman")
    }
    shadowJar {
        archiveBaseName.set("authman")
        archiveClassifier.set("")
        relocate("com.google.gson", "com.iroselle.authman.libs.gson")
        relocate("org.spongepowered.configurate", "com.iroselle.authman.libs.configurate")
        relocate("org.yaml.snakeyaml", "com.iroselle.authman.libs.snakeyaml")
        relocate("io.leangen.geantyref", "com.iroselle.authman.libs.geantyref")
        minimize()
    }
    build {
        dependsOn(shadowJar)
    }
}
