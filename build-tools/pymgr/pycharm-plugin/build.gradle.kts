plugins {
    java
    id("org.jetbrains.intellij.platform") version "2.18.1"
}

group = "dev.zincscale.pymgr"
version = "0.1.0"

repositories {
    mavenCentral()
    intellijPlatform {
        defaultRepositories()
    }
}

dependencies {
    implementation("com.google.code.gson:gson:2.13.2")
    intellijPlatform {
        pycharm("2026.1.4")
        bundledPlugin("PythonCore")
    }
}

intellijPlatform {
    pluginConfiguration {
        ideaVersion {
            sinceBuild = "261"
        }
    }
}

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(25)
    }
}

tasks.withType<JavaCompile>().configureEach {
    sourceCompatibility = "21"
    targetCompatibility = "21"
    options.release = 21
    options.encoding = "UTF-8"
    options.compilerArgs.add("-Xlint:deprecation")
}
