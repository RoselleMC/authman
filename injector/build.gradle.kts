plugins {
    java
    id("com.gradleup.shadow") version "8.3.6"
}

group = "com.iroselle.authman"
version = "0.1.0-dev"

dependencies {
    implementation("net.bytebuddy:byte-buddy:1.17.5")
}

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(17))
    }
}

tasks {
    jar {
        archiveBaseName.set("authman-injector")
        manifest {
            attributes(
                "Premain-Class" to "com.iroselle.authman.injector.AuthmanInjectorAgent",
                "Agent-Class" to "com.iroselle.authman.injector.AuthmanInjectorAgent",
                "Can-Redefine-Classes" to "true",
                "Can-Retransform-Classes" to "true",
            )
        }
    }

    shadowJar {
        archiveBaseName.set("authman-injector")
        archiveClassifier.set("")
        manifest.inheritFrom(jar.get().manifest)
        relocate("net.bytebuddy", "com.iroselle.authman.injector.libs.bytebuddy")
    }

    build {
        dependsOn(shadowJar)
    }
}
