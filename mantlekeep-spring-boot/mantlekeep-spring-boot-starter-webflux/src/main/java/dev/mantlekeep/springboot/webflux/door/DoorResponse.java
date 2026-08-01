package dev.mantlekeep.springboot.webflux.door;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import java.util.List;

/**
 * The door's canonical JSON response, parsed leniently. Package-private — an internal wire
 * shape, not part of the SDK surface. Matches the frozen contract
 * ({@code outcome, token, policyId, reasons[{code,message}], requiredApprovers, expiresAt});
 * stays tolerant of extra fields and of a leaner error shape ({@code reason}/{@code error}).
 */
@JsonIgnoreProperties(ignoreUnknown = true)
record DoorResponse(
        String outcome,
        String token,
        String policyId,
        String expiresAt,
        List<Reason> reasons,
        List<String> requiredApprovers,
        String reason,
        String error) {

    /** One typed reason on the wire: a stable code plus the human message. */
    @JsonIgnoreProperties(ignoreUnknown = true)
    record Reason(String code, String message) {
    }

    static DoorResponse empty() {
        return new DoorResponse(null, null, null, null, null, null, null, null);
    }
}
