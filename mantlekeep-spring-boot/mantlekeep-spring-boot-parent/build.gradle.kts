// Precompiled Gradle convention plugins live here. `kotlin-dsl` turns every
// src/main/kotlin/*.gradle.kts file into a reusable plugin id.
plugins {
    `kotlin-dsl`
}

repositories {
    gradlePluginPortal()
    mavenCentral()
}

dependencies {
    // Versions come from gradle/libs.versions.toml, which also carries the note on why this
    // is here and why it never reaches a module's runtime or published ABI.
    implementation(libs.spotbugs.gradle.plugin)
}
