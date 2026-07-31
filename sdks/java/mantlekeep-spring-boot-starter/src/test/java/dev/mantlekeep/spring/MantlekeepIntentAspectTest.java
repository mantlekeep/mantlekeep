package dev.mantlekeep.spring;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.DoorDeniedException;
import dev.mantlekeep.door.model.Intent;
import dev.mantlekeep.door.model.Subject;
import java.util.List;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;
import org.springframework.context.annotation.AnnotationConfigApplicationContext;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.EnableAspectJAutoProxy;

/**
 * Proves govern-before-execute through REAL Spring AOP weaving (a real context,
 * a real proxy): a deny throws and the method body NEVER runs; an allow proceeds.
 */
class MantlekeepIntentAspectTest {

    /** A sample product service with a governed method. */
    static class DeployService {
        boolean deployed;

        @MantlekeepIntent(action = "service.deploy", resource = "project/demo", goal = "deploy the release")
        public void deploy() {
            deployed = true;
        }

        @MantlekeepIntent(action = "report.view")
        public String viewReport() {
            return "report-body";
        }
    }

    @Configuration
    @EnableAspectJAutoProxy(proxyTargetClass = true)
    static class AspectTestWiring {
        @Bean
        RecordingDoorClient doorClient() {
            return new RecordingDoorClient(List.of("service.deploy"));
        }

        @Bean
        SubjectResolver subjectResolver() {
            return () -> Subject.ofId("user-amy");
        }

        @Bean
        MantlekeepIntentAspect mantleIntentAspect(
                RecordingDoorClient doorClient, SubjectResolver subjectResolver) {
            return new MantlekeepIntentAspect(doorClient, subjectResolver);
        }

        @Bean
        DeployService deployService() {
            return new DeployService();
        }
    }

    private final AnnotationConfigApplicationContext context =
            new AnnotationConfigApplicationContext(AspectTestWiring.class);

    @AfterEach
    void closeContext() {
        context.close();
    }

    @Test
    void denyAbortsBeforeTheMethodBodyRuns() {
        DeployService deployService = context.getBean(DeployService.class);

        DoorDeniedException denial =
                assertThrows(DoorDeniedException.class, deployService::deploy);

        assertFalse(deployService.deployed,
                "govern-before-execute: a denied method body must NEVER run");
        assertTrue(denial.reason().contains("denied by test policy"));
    }

    @Test
    void allowProceedsAndTheIntentCarriedSubjectActionAndGoal() {
        DeployService deployService = context.getBean(DeployService.class);
        RecordingDoorClient doorClient = context.getBean(RecordingDoorClient.class);

        String result = deployService.viewReport();

        assertEquals("report-body", result);
        Intent submitted = doorClient.submittedIntents().get(0);
        assertEquals("report.view", submitted.action());
        assertEquals("user-amy", submitted.subject().id());
        assertFalse(submitted.goal().isBlank(),
                "a blank goal must default to the method signature — the door requires a WHY");
    }

    @Test
    void theDoorIsAskedBeforeEveryInvocationNotOnce() {
        DeployService deployService = context.getBean(DeployService.class);
        RecordingDoorClient doorClient = context.getBean(RecordingDoorClient.class);

        deployService.viewReport();
        deployService.viewReport();

        assertEquals(2, doorClient.submittedIntents().size());
    }
}
