// The shared build convention ("parent") every MantleKeep Spring Boot module applies:
// Java 25 toolchain, group/version, repositories, JUnit 5. Apply with:
//   plugins { id("mantlekeep-spring-boot.java-conventions") }
plugins {
    `java-library`
}

group = "dev.mantlekeep"
version = "0.1.0-rc.3"

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(25)
    }
}

repositories {
    mavenCentral()
}

tasks.withType<Test>().configureEach {
    useJUnitPlatform()
}
