// MantleKeep Spring Boot SDK — the starter family (governed door integration).
// House convention: <name>-parent (build convention) · <name>-dependencies (BOM) ·
// typed <name>-<type> modules. The `-parent` is realized as Gradle convention plugins
// (the idiomatic mapping of a Maven parent POM), supplied by an included build.
rootProject.name = "mantlekeep-spring-boot"

pluginManagement {
    includeBuild("mantlekeep-spring-boot-parent") // supplies the convention plugins ("-parent")
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}

// The pure-JDK door spine (Intent · Decision · DoorClient · DoorConfig) is built by the
// SEPARATE sdks/java Gradle build. Compose it in as source so the starter family consumes
// the ONE definition directly: Gradle substitutes `dev.mantlekeep:mantlekeep-door-client`
// (and its adapter-spi) with the local projects — no publish step, no drift between what we
// build and what we consume. (The Maven build reaches the same artifact from ~/.m2 instead;
// see mantlekeep-spring-boot-dependencies/pom.xml — mvn install sdks/java first.)
includeBuild("../sdks/java")

dependencyResolutionManagement {
    repositories {
        mavenCentral()
    }
}

include(
    "mantlekeep-spring-boot-dependencies",     // BOM / version constraints
    "mantlekeep-spring-boot-core",             // shared, transport-agnostic contracts
    "mantlekeep-spring-boot-starter-webflux",  // reactive starter (Phase 1)
    "mantlekeep-spring-boot-starter-ai",       // AgentPort via the local Claude Code CLI (BYOK adapter)
)

// This composite is the starter family only. Example governed-worker SERVICES that wrap
// the door live in a separate examples repository, not in the generic framework.
