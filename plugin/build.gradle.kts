import com.github.jengelman.gradle.plugins.shadow.tasks.ShadowJar
import org.gradle.language.jvm.tasks.ProcessResources
import org.gradle.api.tasks.compile.JavaCompile
import org.jetbrains.kotlin.gradle.dsl.JvmTarget
import org.jetbrains.kotlin.gradle.tasks.KotlinCompile

plugins {
    kotlin("jvm") version "2.0.21"
    id("com.gradleup.shadow") version "8.3.6"
}

group = "com.iroselle.authman"
version = "0.3.0-dev"

val velocityApi = "com.velocitypowered:velocity-api:3.4.0"
val runtimeSourceSet = sourceSets.create("runtime")
val bridgeSourceSet = sourceSets.create("bridge")

kotlin {
    jvmToolchain(21)
    sourceSets.named("main") {
        kotlin.setSrcDirs(listOf("src/bootstrap/kotlin", "src/protocol/kotlin"))
    }
    sourceSets.named("runtime") {
        kotlin.setSrcDirs(listOf("src/main/kotlin"))
    }
    sourceSets.named("bridge") {
        kotlin.setSrcDirs(listOf("src/bridge/kotlin", "src/protocol/kotlin"))
    }
}

dependencies {
    compileOnly(velocityApi)
    implementation("com.google.code.gson:gson:2.11.0")
    implementation("org.spongepowered:configurate-yaml:4.2.0")

    add("runtimeCompileOnly", velocityApi)
    add("runtimeCompileOnly", sourceSets.main.map { it.output })
    add("runtimeImplementation", "com.google.code.gson:gson:2.11.0")

    add("bridgeCompileOnly", "io.papermc.paper:paper-api:1.20.4-R0.1-SNAPSHOT")
    add("bridgeImplementation", "com.google.code.gson:gson:2.11.0")

    testImplementation(kotlin("test"))
    testImplementation(velocityApi)
    testImplementation(runtimeSourceSet.output)
    testImplementation("org.junit.jupiter:junit-jupiter:5.11.4")
    testRuntimeOnly("org.slf4j:slf4j-simple:2.0.16")
}

tasks.named("compileRuntimeKotlin") {
    dependsOn(tasks.named("compileKotlin"))
}

tasks.named("compileTestKotlin") {
    dependsOn(tasks.named("compileRuntimeKotlin"))
}

tasks.test {
    useJUnitPlatform()
}

tasks.named<KotlinCompile>("compileBridgeKotlin") {
    compilerOptions.jvmTarget.set(JvmTarget.JVM_17)
}

tasks.named<JavaCompile>("compileBridgeJava") {
    options.release.set(17)
}

tasks.named<ProcessResources>("processBridgeResources") {
    filesMatching("plugin.yml") {
        expand("version" to project.version.toString())
    }
}

tasks.jar {
    archiveBaseName.set("authman-bootstrap")
    archiveVersion.set("")
    archiveClassifier.set("plain")
}

tasks.shadowJar {
    archiveBaseName.set("authman-bootstrap")
    archiveVersion.set("")
    archiveClassifier.set("")
    relocate("com.google.gson", "com.iroselle.authman.bootstrap.libs.gson")
    relocate("org.spongepowered.configurate", "com.iroselle.authman.bootstrap.libs.configurate")
    relocate("org.yaml.snakeyaml", "com.iroselle.authman.bootstrap.libs.snakeyaml")
    relocate("io.leangen.geantyref", "com.iroselle.authman.bootstrap.libs.geantyref")
}

val runtimeShadowJar by tasks.registering(ShadowJar::class) {
    group = "build"
    description = "Builds the hot-loadable Authman Velocity runtime module."
    archiveBaseName.set("authman-runtime")
    archiveClassifier.set("")
    from(runtimeSourceSet.output)
    configurations = listOf(project.configurations.getByName("runtimeRuntimeClasspath"))
    relocate("com.google.gson", "com.iroselle.authman.runtime.libs.gson")
    manifest {
        attributes(
            "Authman-Runtime-Entrypoint" to "com.iroselle.authman.AuthmanPlugin",
            "Authman-Runtime-Version" to project.version.toString(),
            "Authman-Runtime-Api" to "2",
            "Authman-Runtime-Contract" to "authman.velocity.runtime.v1",
            "Implementation-Title" to "Authman Velocity Runtime",
            "Implementation-Version" to project.version.toString(),
        )
    }
}

val bridgeShadowJar by tasks.registering(ShadowJar::class) {
    group = "build"
    description = "Builds the optional Authman Bukkit/Paper/Folia command bridge."
    archiveBaseName.set("authman-bridge")
    archiveClassifier.set("")
    from(bridgeSourceSet.output)
    configurations = listOf(project.configurations.getByName("bridgeRuntimeClasspath"))
    relocate("com.google.gson", "com.iroselle.authman.bridge.libs.gson")
}

tasks.build {
    dependsOn(tasks.shadowJar, runtimeShadowJar, bridgeShadowJar)
}
