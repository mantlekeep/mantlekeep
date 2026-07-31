package dev.mantlekeep.door.model;

/**
 * The one config flip (composition-model §4b — the embedded-DB pattern): the same
 * product binary either CALLS the shared door or CARRIES its own. Products never
 * change; only the mode.
 */
public enum DoorMode {

    /** The remote door over HTTP — production: scalable, one shared chain. */
    SERVICE,

    /** The in-process native core — tests, dev, and the sovereign air-gap zone. */
    EMBEDDED
}
