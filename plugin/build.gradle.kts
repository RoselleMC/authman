plugins {
    kotlin("jvm") version "2.0.21"
    id("com.gradleup.shadow") version "8.3.6"
}

group = "mc.roselle.authman"
version = "0.1.0-dev"

dependencies {
    compileOnly("com.velocitypowered:velocity-api:3.5.0-SNAPSHOT")
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
        minimize()
    }
    build {
        dependsOn(shadowJar)
    }
}

