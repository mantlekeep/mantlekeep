package dev.mantlekeep.spi;

/**
 * The execution port. A host DECIDES through the door and delegates the effect here,
 * so the layer that governs never also runs the work. Adapters: a container-job
 * emitter, a CI trigger, a self-pod runner — the host knows only this port.
 *
 * <p><b>Reach this through {@code GovernedWorker}, not directly.</b> This interface is
 * the adapter's side of the contract; it performs an effect and asks nothing. What puts
 * the door in front of it is the framework's wrapper, whose decide-then-dispatch
 * sequence is {@code final}. A product given the raw port would be responsible for
 * remembering to govern, and governance that depends on remembering is eventually not
 * done.
 */
public interface WorkerPort {

    /**
     * Dispatches one unit of work. The request carries the execution token the door
     * issued, which names the decision that authorised it.
     *
     * <p>The token is EVIDENCE, not a key: it is currently unsigned and unverified, so
     * an adapter cannot prove anything from it and should not treat its presence as
     * authorisation. Authorisation happened before this call, in the wrapper. For a
     * control that makes execution impossible without a decision, rather than merely
     * ordered by convention, see {@code docs/credential-brokering.md}.
     *
     * @param workRequestJson the work request (JSON, per the door's versioned contract)
     * @return a dispatch receipt (JSON): the handle/id the host can track
     */
    String dispatch(String workRequestJson);
}
