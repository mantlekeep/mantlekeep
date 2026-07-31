package dev.mantlekeep.springboot.webflux.door;

import static org.junit.jupiter.api.Assertions.assertEquals;

import dev.mantlekeep.springboot.door.DoorProperties;
import org.junit.jupiter.api.Test;

/**
 * A service acting for a person must send TWO identities, and the door needs both:
 * who the application is, and who it acts for.
 *
 * <p>This is the case that was broken. The client sent only the on-behalf-of header and
 * carried its own identity as a bearer token, which the door does not read — so the door
 * saw an unidentified caller and refused every request. Nothing in the client's own tests
 * noticed, because they only ever asserted the on-behalf-of header.
 *
 * <p>The properties are asserted directly rather than through a live exchange: the defect
 * was a missing VALUE, not faulty transport, and this fails fast and reads clearly.
 */
class ServiceIdentityHeaderTest {

    private static DoorProperties properties(String serviceUser, String serviceUserHeader) {
        return new DoorProperties("http://door.local", "/api/govern", "",
                serviceUser, serviceUserHeader, null, null);
    }

    @Test
    void theServiceIdentityHeaderDefaultsToTheOneTheDoorReads() {
        // The door's authenticated-caller header. If the client's default and the door's
        // default disagree, delegation fails with an unhelpful 401.
        assertEquals("X-Mantlekeep-User", properties("session-service", null).serviceUserHeader());
    }

    @Test
    void theServiceNameIsCarriedSoTheDoorCanAuthenticateTheApplication() {
        assertEquals("session-service", properties("session-service", null).serviceUser());
    }

    @Test
    void aServiceThatNeverActsForAnyoneNeedNotConfigureAnIdentity() {
        // Blank is legitimate: a caller acting only as itself is identified by whatever sits
        // in front of the door. It must not become the literal string "null".
        assertEquals("", properties(null, null).serviceUser());
    }

    @Test
    void theHeaderNameCanBeOverriddenForADoorThatReadsADifferentOne() {
        assertEquals("X-Gateway-User", properties("svc", "X-Gateway-User").serviceUserHeader());
    }

    @Test
    void theOnBehalfOfHeaderIsTheDocumentedName() {
        // The other half of the pair — pinned for the same reason the caller header is.
        assertEquals("X-Mantlekeep-On-Behalf-Of", WebClientDoorClient.ON_BEHALF_OF);
    }
}
