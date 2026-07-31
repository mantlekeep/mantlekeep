package dev.mantlekeep.spring;

import dev.mantlekeep.door.AdapterCatalog;
import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.GovernedWorker;
import dev.mantlekeep.door.DoorClientFactory;
import dev.mantlekeep.door.model.Subject;
import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.WorkerPort;
import org.springframework.boot.autoconfigure.AutoConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;

/**
 * The no-code wiring: a product that depends on this starter and sets
 * {@code mantlekeep.door.mode} gets the {@link DoorClient} bean and govern-before-execute
 * interception — built entirely from properties via {@link DoorClientFactory}, with
 * every adapter selected from the ServiceLoader-registered set (see the SECURITY
 * note on {@code AdapterCatalog}). No {@code mantlekeep.door.mode} property → the
 * starter stays completely inert.
 *
 * <p>Every bean is {@code @ConditionalOnMissingBean}: a product can override any
 * piece (most commonly {@link SubjectResolver}, backed by its SSO principal) by
 * declaring its own bean — extend by DI, never by forking the starter.
 */
@AutoConfiguration
@ConditionalOnProperty(prefix = "mantlekeep.door", name = "mode")
@EnableConfigurationProperties(MantlekeepProperties.class)
public class MantlekeepAutoConfiguration {

    @Bean(destroyMethod = "close")
    @ConditionalOnMissingBean
    public DoorClient doorClient(MantlekeepProperties properties) {
        return DoorClientFactory.create(properties.toDoorConfig());
    }

    @Bean
    @ConditionalOnMissingBean
    public SubjectResolver subjectResolver(MantlekeepProperties properties) {
        Subject configuredSubject = properties.subject().isBlank()
                ? Subject.anonymous()
                : Subject.ofId(properties.subject());
        return () -> configuredSubject;
    }

    @Bean
    @ConditionalOnMissingBean
    public MantlekeepIntentAspect mantleIntentAspect(
            DoorClient doorClient, SubjectResolver subjectResolver) {
        return new MantlekeepIntentAspect(doorClient, subjectResolver);
    }

    /**
     * The governed execution path — the only worker a product is given.
     *
     * <p>Note what is NOT published here: the raw {@link dev.mantlekeep.spi.WorkerPort}.
     * The adapter is resolved from the registered SPI set, so it never enters the
     * application context and {@code @Autowired WorkerPort} does not resolve. A product
     * therefore cannot casually inject the unwrapped executor — not because it was asked
     * not to, but because that bean does not exist.
     *
     * <p>Registered only when {@code mantlekeep.adapters.worker} names a worker, since a
     * product that dispatches nothing needs no executor.
     */
    @Bean
    @ConditionalOnMissingBean
    @ConditionalOnProperty(prefix = "mantlekeep.adapters", name = "worker")
    public GovernedWorker governedWorker(DoorClient doorClient, MantlekeepProperties properties) {
        String workerName = properties.adapters().get("worker");
        Object adapter = AdapterCatalog.discover()
                .select(AdapterKind.WORKER, workerName, properties.adapters());
        return new GovernedWorker(doorClient, (WorkerPort) adapter);
    }
}
