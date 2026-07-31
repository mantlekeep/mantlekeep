package dev.mantlekeep.adapter.policy.allowlist;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.door.AdapterCatalog;
import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.PolicyEvaluator;
import dev.mantlekeep.spi.PolicyVerdict;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

/**
 * Proves this SECOND, independent adapter jar rides the same SPI: ServiceLoader
 * discovery via the real {@link AdapterCatalog}, selection by config name, adapter
 * behaviour driven through the {@link PolicyEvaluator} port, and fail-fast rejection
 * of any name outside the registered set.
 */
class AllowlistPolicyProviderTest {

    private final AdapterCatalog catalog = AdapterCatalog.discover();

    @Test
    void serviceLoaderDiscoversThisAdapterJar() {
        assertEquals(List.of(AllowlistPolicyProvider.NAME),
                catalog.registeredNames(AdapterKind.POLICY_EVALUATOR));
    }

    @Test
    void configNameSelectsAConfiguredEvaluator() {
        Object adapter = catalog.select(AdapterKind.POLICY_EVALUATOR, "allowlist",
                Map.of(AllowlistPolicyProvider.ALLOWED_ACTIONS_KEY, "job.build, job.test"));

        PolicyEvaluator evaluator = assertInstanceOf(PolicyEvaluator.class, adapter);
        assertTrue(evaluator.evaluate("dev-team", "job.build", Map.of()).allowed());

        PolicyVerdict promoteVerdict =
                evaluator.evaluate("dev-team", "job.promote", Map.of());
        assertFalse(promoteVerdict.allowed());
        assertTrue(promoteVerdict.reason().contains("job.promote"));
    }

    @Test
    void missingConfigMeansDenyByDefault() {
        PolicyEvaluator evaluator = (PolicyEvaluator)
                catalog.select(AdapterKind.POLICY_EVALUATOR, "allowlist", Map.of());
        assertFalse(evaluator.evaluate("anyone", "job.build", Map.of()).allowed());
    }

    @Test
    void anUnknownNameFailsFastListingTheRegisteredMenu() {
        // Config can only pick among registered providers — a class-looking name is
        // rejected as an unknown NAME, never resolved as a class.
        IllegalArgumentException failure = assertThrows(IllegalArgumentException.class,
                () -> catalog.select(AdapterKind.POLICY_EVALUATOR, "com.evil.Backdoor", Map.of()));
        assertTrue(failure.getMessage().contains("com.evil.Backdoor"));
        assertTrue(failure.getMessage().contains("allowlist"));
        assertTrue(failure.getMessage().contains("never loaded by class name"));
    }
}
