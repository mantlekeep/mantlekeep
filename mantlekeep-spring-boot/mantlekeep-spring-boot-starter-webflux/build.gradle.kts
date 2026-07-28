// The reactive starter: a WebClient-based DoorClient (Mono), a reactive @MantlekeepIntent
// aspect, and Spring Boot auto-configuration. Depends on -core.
plugins {
    id("mantlekeep-spring-boot.java-conventions")
}

dependencies {
    api(platform(project(":mantlekeep-spring-boot-dependencies")))
    annotationProcessor(platform(project(":mantlekeep-spring-boot-dependencies"))) // BOM pins the processor version too
    api(project(":mantlekeep-spring-boot-core"))

    implementation("org.springframework.boot:spring-boot-starter-webflux")
    implementation("org.aspectj:aspectjweaver") // @MantlekeepIntent aspect (Spring Boot 4 dropped starter-aop; spring-aop comes via context)
    annotationProcessor("org.springframework.boot:spring-boot-configuration-processor")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation("io.projectreactor:reactor-test")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher") // Gradle 9 needs the launcher explicitly
}
