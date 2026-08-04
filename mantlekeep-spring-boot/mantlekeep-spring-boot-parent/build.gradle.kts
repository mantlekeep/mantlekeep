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
    // Puts the SpotBugs Gradle plugin on the convention-plugin classpath so the precompiled
    // `mantlekeep-spring-boot.java-conventions` script can apply `id("com.github.spotbugs")`.
    // BUILD-only: it never reaches any module's runtime or published ABI.
    implementation("com.github.spotbugs.snom:spotbugs-gradle-plugin:6.5.9")
}
