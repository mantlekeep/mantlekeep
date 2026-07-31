package dev.mantlekeep.door;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

import dev.mantlekeep.spi.AdapterKind;
import dev.mantlekeep.spi.AdapterProvider;
import dev.mantlekeep.spi.PolicyEvaluator;
import dev.mantlekeep.spi.PolicyVerdict;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class AdapterCatalogTest {

    /** A well-behaved registered adapter: allow-everything policy evaluator. */
    private static final class AllowAllPolicyProvider implements AdapterProvider {
        @Override
        public String name() {
            return "allow-all";
        }

        @Override
        public AdapterKind kind() {
            return AdapterKind.POLICY_EVALUATOR;
        }

        @Override
        public Object create(Map<String, String> configuration) {
            return (PolicyEvaluator) (subjectId, action, attributes) -> PolicyVerdict.allow();
        }
    }

    /** A misbehaving provider: claims STORE but returns something else entirely. */
    private static final class LyingStoreProvider implements AdapterProvider {
        @Override
        public String name() {
            return "lying-store";
        }

        @Override
        public AdapterKind kind() {
            return AdapterKind.STORE;
        }

        @Override
        public Object create(Map<String, String> configuration) {
            return "not a StorePort at all";
        }
    }

    private final AdapterCatalog catalog =
            new AdapterCatalog(List.of(new AllowAllPolicyProvider(), new LyingStoreProvider()));

    @Test
    void selectsARegisteredAdapterByKindAndName() {
        Object adapter = catalog.select(AdapterKind.POLICY_EVALUATOR, "allow-all", Map.of());
        assertInstanceOf(PolicyEvaluator.class, adapter);
    }

    @Test
    void unknownNameFailsListingTheRegisteredMenu() {
        IllegalArgumentException failure = assertThrows(IllegalArgumentException.class,
                () -> catalog.select(AdapterKind.POLICY_EVALUATOR, "opa-wasm", Map.of()));
        assertTrue(failure.getMessage().contains("allow-all"));
        assertTrue(failure.getMessage().contains("never loaded by class name"));
    }

    @Test
    void aProviderReturningTheWrongTypeIsRejected() {
        IllegalStateException failure = assertThrows(IllegalStateException.class,
                () -> catalog.select(AdapterKind.STORE, "lying-store", Map.of()));
        assertTrue(failure.getMessage().contains("does not implement"));
    }

    @Test
    void registeredNamesArePerKind() {
        assertEquals(List.of("allow-all"), catalog.registeredNames(AdapterKind.POLICY_EVALUATOR));
        assertEquals(List.of(), catalog.registeredNames(AdapterKind.WORKER));
    }
}
