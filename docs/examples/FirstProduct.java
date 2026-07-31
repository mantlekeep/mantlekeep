// FirstProduct.java — build your first governed product on MantleKeep.
//
// A minimal governed worker assembled from the SDK's PORTS and two example
// adapters, plus one adapter you write yourself (a WorkerPort). It shows the
// single rule the whole framework serves: GOVERN BEFORE YOU EXECUTE.
//
//   1. an intent arrives (who + what action + why)
//   2. the door asks the PolicyEvaluator port for a verdict
//   3. every verdict — allow AND deny — is appended to the hash-chained audit
//      store (a deny is evidence too)
//   4. only on ALLOW does the door dispatch to the WorkerPort; a deny aborts
//      before any side effect runs
//
// Single-file program: compile and run with `java` (JDK 25+). See
// docs/build-your-first-product.md for the exact commands and expected output.

import dev.mantlekeep.spi.PolicyEvaluator;
import dev.mantlekeep.spi.PolicyVerdict;
import dev.mantlekeep.spi.StorePort;
import dev.mantlekeep.spi.WorkerPort;
import dev.mantlekeep.adapter.policy.allowlist.AllowlistPolicyEvaluator;
import dev.mantlekeep.adapter.store.inmemory.InMemoryStore;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.List;
import java.util.Map;
import java.util.Set;

public class FirstProduct {

    // ── 1. YOUR adapter: implement the WorkerPort ────────────────────────────
    // The core knows only the port; the backend knowledge lives here. A real
    // adapter emits a k8s Job, triggers Jenkins, or runs a pod. This one just
    // returns a dispatch receipt — enough to prove the door only ever calls it
    // AFTER an allow.
    static final class SessionWorker implements WorkerPort {
        @Override
        public String dispatch(String workRequestJson) {
            System.out.println("    [worker] EXECUTED " + workRequestJson);
            return "{\"receipt\":\"session-0001\",\"status\":\"started\"}";
        }
    }

    // ── 2. The governance loop: govern → record → (only then) execute ─────────
    // This is the shape of every governed host: it DECIDES through the policy
    // port, RECORDS the decision on the append-only hash-chain, and EXECUTES
    // through the worker port only when the verdict is allow.
    static final class Door {
        private final PolicyEvaluator policy;   // example adapter: allowlist
        private final StorePort chain;          // example adapter: in-memory store
        private final WorkerPort worker;        // your adapter
        private String previousHash = "GENESIS";

        Door(PolicyEvaluator policy, StorePort chain, WorkerPort worker) {
            this.policy = policy;
            this.chain = chain;
            this.worker = worker;
        }

        /** Submit one intent. Returns the worker receipt on allow, or null on deny. */
        String submit(String subjectId, String action, String goal, Map<String, String> attributes) {
            // GOVERN FIRST — ask the policy port before doing anything.
            PolicyVerdict verdict = policy.evaluate(subjectId, action, attributes);

            // RECORD — link each record to the prior record's hash (tamper-evident).
            String decision = verdict.allowed() ? "allow" : "deny";
            String record = "{\"subject\":\"" + subjectId + "\",\"action\":\"" + action
                    + "\",\"decision\":\"" + decision + "\",\"reason\":\"" + verdict.reason()
                    + "\",\"prev\":\"" + previousHash + "\"}";
            chain.append(record);
            previousHash = sha256(previousHash + record);

            if (!verdict.allowed()) {
                // DENY ABORTS — the worker is never reached, no side effect runs.
                System.out.println("  DENY  " + subjectId + " → " + action + "  (" + verdict.reason() + ")");
                return null;
            }
            // ALLOW — and only now does the effect run, through the worker port.
            System.out.println("  ALLOW " + subjectId + " → " + action);
            return worker.dispatch("{\"action\":\"" + action + "\",\"goal\":\"" + goal + "\"}");
        }
    }

    public static void main(String[] args) throws Exception {
        // ── 3. WIRE the door from the ports + adapters ────────────────────────
        // The policy allows exactly two actions; everything else is denied by
        // default. Swap either adapter for a real backend without touching this
        // wiring — that is the ports-and-adapters seam.
        PolicyEvaluator policy = new AllowlistPolicyEvaluator(
                Set.of("session.start", "session.stop"));
        StorePort chain = new InMemoryStore();
        WorkerPort worker = new SessionWorker();
        Door door = new Door(policy, chain, worker);

        System.out.println("MantleKeep — first governed product");
        System.out.println("──────────────────────────────────────────");

        // ── 4. SUBMIT intents and watch govern-before-execute ────────────────
        door.submit("dev-alice", "session.start", "open a work session",
                Map.of("env", "DEV"));                 // on the allowlist → executes
        door.submit("dev-alice", "session.stop", "close the session",
                Map.of("env", "DEV"));                 // on the allowlist → executes
        door.submit("ci-agent", "session.approve", "approve my own session",
                Map.of("env", "PROD"));                // NOT on the allowlist → denied, never executes

        // ── 5. READ the hash-chain — every decision, allow and deny alike ─────
        System.out.println("──────────────────────────────────────────");
        System.out.println("audit hash-chain (" + chain.readAll().size() + " records, oldest first):");
        List<String> records = chain.readAll();
        for (int i = 0; i < records.size(); i++) {
            System.out.println("  " + (i + 1) + ". " + records.get(i));
        }
        System.out.println("chain intact: " + verify(chain.readAll()));
    }

    // ── the tamper-evident check: re-walk the chain, recompute each link ──────
    private static boolean verify(List<String> records) {
        String prev = "GENESIS";
        for (String record : records) {
            if (!record.contains("\"prev\":\"" + prev + "\"")) {
                return false;   // a record's back-link does not match → tampered
            }
            prev = sha256(prev + record);
        }
        return true;
    }

    private static String sha256(String input) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256")
                    .digest(input.getBytes(StandardCharsets.UTF_8));
            StringBuilder hex = new StringBuilder(64);
            for (byte b : digest) {
                hex.append(Character.forDigit((b >> 4) & 0xF, 16));
                hex.append(Character.forDigit(b & 0xF, 16));
            }
            return hex.toString();
        } catch (Exception e) {
            throw new IllegalStateException("SHA-256 unavailable", e);
        }
    }
}
