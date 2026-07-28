package dev.mantlekeep.springboot.webflux.door;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import java.util.List;

/**
 * The door's JSON response, parsed leniently. Package-private — an internal wire shape,
 * not part of the SDK surface. Tolerant of extra fields and of either the allow shape
 * ({@code decision}/{@code token}) or an error shape ({@code reason}/{@code error}).
 */
@JsonIgnoreProperties(ignoreUnknown = true)
record DoorResponse(
        String decision,
        String token,
        String expires,
        String policyId,
        List<String> reasons,
        List<String> requiredApprovers,
        String reason,
        String error) {

    static DoorResponse empty() {
        return new DoorResponse(null, null, null, null, null, null, null, null);
    }
}
