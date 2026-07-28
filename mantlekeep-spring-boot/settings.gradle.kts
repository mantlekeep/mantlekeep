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
