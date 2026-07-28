package dev.mantlekeep.spring;

import dev.mantlekeep.door.DoorClient;
import dev.mantlekeep.door.model.AuditRecord;
import dev.mantlekeep.door.model.Decision;
import dev.mantlekeep.door.model.Intent;
import java.util.ArrayList;
import java.util.List;

/** A test door: scripted verdict per action, every submitted intent recorded. */
final class RecordingDoorClient implements DoorClient {

    private final List<String> deniedActions;
    private final List<Intent> submittedIntents = new ArrayList<>();

    RecordingDoorClient(List<String> deniedActions) {
        this.deniedActions = List.copyOf(deniedActions);
    }

    @Override
    public Decision decide(Intent intent) {
        submittedIntents.add(intent);
        return deniedActions.contains(intent.action())
                ? Decision.deny("denied by test policy: " + intent.action())
                : Decision.allow("TEST-TOKEN");
    }

    @Override
    public List<AuditRecord> audit() {
        return List.of();
    }

    @Override
    public boolean verify() {
        return true;
    }

    @Override
    public void close() {
    }

    List<Intent> submittedIntents() {
        return submittedIntents;
    }
}
