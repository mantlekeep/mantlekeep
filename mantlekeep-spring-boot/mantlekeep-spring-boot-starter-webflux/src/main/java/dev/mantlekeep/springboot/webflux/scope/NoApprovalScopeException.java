package dev.mantlekeep.springboot.webflux.scope;

/**
 * Thrown when governed work is asked to run with no approval scope established — a blank approval
 * token. It is the fail-closed signal: the steps never start, because nothing proved the governing
 * transition was allowed. A product maps this to a refusal (not a 500), the same as a door denial.
 */
public class NoApprovalScopeException extends RuntimeException {

    public NoApprovalScopeException() {
        super("no approval scope: governed execution requires an allowed transition's token");
    }
}
