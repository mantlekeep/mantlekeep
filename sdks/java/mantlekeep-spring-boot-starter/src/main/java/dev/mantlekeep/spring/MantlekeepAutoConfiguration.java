package dev.mantlekeep.spring;

import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.DoorClientFactory;
import dev.mantlekeep.door.model.Subject;
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
}
