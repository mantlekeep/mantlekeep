// Shared, transport-agnostic contracts: DoorClient (interface), Intent/Decision,
// @MantlekeepIntent, AgentPort. Kept minimal — only Reactor for the Mono/Flux in signatures.
plugins {
    id("mantlekeep-spring-boot.java-conventions")
}

dependencies {
    api(platform(project(":mantlekeep-spring-boot-dependencies")))
    api("io.projectreactor:reactor-core")

    testImplementation("org.junit.jupiter:junit-jupiter")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
}
