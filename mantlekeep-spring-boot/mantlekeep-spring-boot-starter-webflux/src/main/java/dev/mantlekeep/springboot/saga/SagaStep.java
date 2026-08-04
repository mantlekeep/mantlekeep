package dev.mantlekeep.springboot.saga;

/**
 * One entry in a governed order's saga timeline — the runtime counterpart to the audit chain,
 * shared by every product on the starter. The chain records the DECISION at the door; this records
 * what the executor actually DID: which environment, the real command, the outcome. It answers an
 * operator's "what ran, where, did it roll back" without exposing secrets (the command is the safe
 * argv; secret-bearing values go through a file, never here).
 *
 * @param operationId the async operation this step belongs to (correlates to the ledger entry)
 * @param subject     what the order targets — a resource id, a target ref, a run
 * @param at          epoch millis when the step was recorded
 * @param step        the phase — e.g. {@code requested}, {@code executed}, {@code compensated}
 * @param status      {@code accepted} | {@code succeeded} | {@code failed}
 * @param detail      human-readable: the environment and the real command / result (no secrets)
 */
public record SagaStep(String operationId, String subject, long at, String step, String status,
        String detail) {
}
