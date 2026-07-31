package dev.mantlekeep.springboot.webflux.identity;

import static org.junit.jupiter.api.Assertions.assertEquals;

import org.junit.jupiter.api.Test;

/**
 * The identity header names are a WIRE CONTRACT: a gateway sets them and this service
 * reads them, so they must match the documented names exactly and byte-for-byte.
 *
 * <p>This test exists because they once did not. A repository-wide rename applied twice
 * produced {@code X-Mantlekeepkeep-User}, which compiled, passed every behavioural test
 * (the tests had been renamed too, so they agreed with the bug), and would have refused
 * every request from a caller following the documentation. Nothing caught it, because
 * every check that could have was renamed alongside the thing it was checking.
 *
 * <p>Pinning the literal strings here breaks that symmetry: a rename that touches these
 * values has to change a test that spells out what the value must be.
 */
class IdentityHeaderNamesTest {

    @Test
    void defaultHeaderNamesMatchTheDocumentedWireContract() {
        IdentityProperties properties = new IdentityProperties(null, null, null);

        assertEquals("X-Mantlekeep-User", properties.userHeader(),
                "the caller header is documented as X-Mantlekeep-User; a gateway sets it");
        assertEquals("X-Mantlekeep-Roles", properties.rolesHeader(),
                "the roles header is documented as X-Mantlekeep-Roles");
    }
}
