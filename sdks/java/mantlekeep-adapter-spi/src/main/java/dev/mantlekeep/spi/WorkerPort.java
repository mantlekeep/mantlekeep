package dev.mantlekeep.spi;

/**
 * The execution port — govern-never-execute made structural. A control-plane host
 * DECIDES through the door and then delegates the effect here; it never runs work
 * inline. Adapters: a k8s Job emitter, a Jenkins trigger, a self-pod runner —
 * the host knows only this port.
 */
public interface WorkerPort {

    /**
     * Dispatches one unit of governed work. The request carries the execution token
     * the door issued — a worker without a token is a worker the door never allowed.
     *
     * @param workRequestJson the work request (JSON, per the door's versioned contract)
     * @return a dispatch receipt (JSON): the handle/id the host can track
     */
    String dispatch(String workRequestJson);
}
