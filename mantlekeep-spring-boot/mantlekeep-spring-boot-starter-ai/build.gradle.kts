// The AI starter: an AgentPort backed by the local Claude Code CLI (`claude -p`),
// so a governed loop can draft roles through the developer's own headless harness.
// The BYOK seam stays pluggable — this is one adapter. Depends on -core.
plugins {
    id("mantlekeep-spring-boot.java-conventions")
}

// Declarations are grouped by configuration — every api together, every implementation
// together — so a reader can see the whole of what this module exposes, or consumes, or
// only needs at build time, without scanning past the other kinds.
dependencies {
    api(platform(project(":mantlekeep-spring-boot-dependencies")))
    api(project(":mantlekeep-spring-boot-core"))
    api("io.projectreactor:reactor-core") // Mono/Flux on the AgentPort adapter surface

    implementation("com.fasterxml.jackson.core:jackson-databind") // parse the CLI's stream-json deltas
    implementation("org.springframework.boot:spring-boot-autoconfigure")
    implementation("org.springframework.boot:spring-boot") // @ConfigurationProperties binding

    annotationProcessor(platform(project(":mantlekeep-spring-boot-dependencies"))) // BOM pins the processor version too
    annotationProcessor("org.springframework.boot:spring-boot-configuration-processor")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation("io.projectreactor:reactor-test")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher") // Gradle 9 needs the launcher explicitly
}
