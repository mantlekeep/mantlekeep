// The BOM ("-dependencies"): a java-platform that pins versions for every module +
// imports the Spring Boot platform, so modules declare dependencies without versions.
plugins {
    `java-platform`
}

group = "dev.mantlekeep"
version = "0.1.1-SNAPSHOT"

javaPlatform {
    allowDependencies() // permits importing the Spring Boot BOM below
}

dependencies {
    api(platform("org.springframework.boot:spring-boot-dependencies:4.1.0"))
    constraints {
        // MantleKeep module versions are pinned here as the family grows.
    }
}
