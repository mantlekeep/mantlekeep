// The reactive starter: a WebClient-based DoorClient (Mono), a reactive @MantlekeepIntent
// aspect, and Spring Boot auto-configuration. Depends on -core.
plugins {
    id("mantlekeep-spring-boot.java-conventions")
}

// Declarations are grouped by configuration — every api together, every implementation
// together — so a reader can see the whole of what this module exposes, or consumes, or
// only needs at build time, without scanning past the other kinds.
dependencies {
    api(platform(project(":mantlekeep-spring-boot-dependencies")))
    api(project(":mantlekeep-spring-boot-core"))

    implementation("org.springframework.boot:spring-boot-starter-webflux")
    implementation("org.aspectj:aspectjweaver") // @MantlekeepIntent aspect (Spring Boot 4 dropped starter-aop; spring-aop comes via context)

    annotationProcessor(platform(project(":mantlekeep-spring-boot-dependencies"))) // BOM pins the processor version too
    annotationProcessor("org.springframework.boot:spring-boot-configuration-processor")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation("io.projectreactor:reactor-test")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher") // Gradle 9 needs the launcher explicitly
}
