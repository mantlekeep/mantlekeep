// Precompiled Gradle convention plugins live here. `kotlin-dsl` turns every
// src/main/kotlin/*.gradle.kts file into a reusable plugin id.
plugins {
    `kotlin-dsl`
}

repositories {
    gradlePluginPortal()
    mavenCentral()
}
