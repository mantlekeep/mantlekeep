// The shared build convention ("parent") every MantleKeep Spring Boot module applies:
// Java 25 toolchain, group/version, repositories, JUnit 5. Apply with:
//   plugins { id("mantlekeep-spring-boot.java-conventions") }
import com.github.spotbugs.snom.Confidence
import com.github.spotbugs.snom.Effort
import com.github.spotbugs.snom.SpotBugsTask

plugins {
    `java-library`
    id("com.github.spotbugs")
}

group = "dev.mantlekeep"
version = "0.1.0"

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(25)
    }
}

repositories {
    mavenCentral()
}

// SpotBugs + FindSecBugs on every starter-family module. effort=max, reportLevel=low so the
// security scan sees everything; HTML (human) + XML (CI) reports. The build FAILS on any remaining
// finding (spotbugs default) — false positives are justified out in the shared spotbugs-exclude.xml,
// never blanket-suppressed. BUILD-only: nothing reaches a module's runtime or published ABI.
spotbugs {
    effort = Effort.MAX
    reportLevel = Confidence.LOW
    excludeFilter = rootProject.file("spotbugs-exclude.xml")
}

dependencies {
    "spotbugsPlugins"("com.h3xstream.findsecbugs:findsecbugs-plugin:1.14.0")
}

tasks.withType<SpotBugsTask>().configureEach {
    reports.create("html") { required = true }
    reports.create("xml") { required = true }
}

// The security gate scans PRODUCTION (main) sources — the shipped surface. Test sources are not
// published and are out of scope; leaving spotbugsTest wired into `check` would make the unrelated
// `gradle build` break on test-only patterns. spotbugsMain remains the gate.
tasks.named("spotbugsTest") { enabled = false }

tasks.withType<Test>().configureEach {
    useJUnitPlatform()
}
