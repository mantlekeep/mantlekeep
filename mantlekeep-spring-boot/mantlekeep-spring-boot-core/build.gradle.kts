// Shared, transport-agnostic contracts: DoorClient (interface), Intent/Decision,
// @MantlekeepIntent, AgentPort. Kept minimal — only Reactor for the Mono/Flux in signatures.
plugins {
    id("mantlekeep-spring-boot.java-conventions")
}

dependencies {
    api(platform(project(":mantlekeep-spring-boot-dependencies")))
    // The pure-JDK door spine — the ONE definition of Intent · Decision · DoorClient ·
    // DoorConfig. Composed in from sdks/java (see settings.gradle.kts includeBuild), so the
    // starter family adapts the spine rather than re-declaring it.
    api("dev.mantlekeep:mantlekeep-door-client:0.1.0-rc.6")
    api("io.projectreactor:reactor-core")

    testImplementation("org.junit.jupiter:junit-jupiter")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}
