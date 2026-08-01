# Reusable execution workers — node reuse, DinD, and Dagger

**The worry:** a pod per pipeline step is slow to start and wasteful — each step waits on scheduling and
image pulls. A conventional CI agent (Jenkins-style) runs a whole job on one warm node and feels fast for
exactly that reason. Some deployments want that back, and to run containers-inside-containers (podman/DinD,
or a Dagger BuildKit engine) for cost and flexibility.

**The answer: this is a `WorkerPort` adapter choice, and it does not touch governance.** The door decides
*whether* a step runs; the worker decides *where and how*. So "reuse the node" and "run steps as containers
in a warm engine" are adapters you select by config — governance is unchanged either way. That is what the
port is for.

---

## 1. Two boundaries, already separated (see `behaviours.md` §6b)

| | Grain | Cost |
|---|---|---|
| **Governance** — the door call | per phase | milliseconds |
| **Execution** — the substrate | per group | scheduling + image pull + start (tens of seconds) |

Governing finely is cheap; executing finely is expensive. A reusable worker keeps execution coarse (one
warm unit per branch or run) while governance stays fine (a decision per phase). Coupling them is what made
per-step pods look like the only option.

## 2. The adapter spectrum

All implement the same `WorkerPort`; the door sees none of the difference.

| Adapter | Substrate | Startup | Isolation | Fits |
|---|---|---|---|---|
| pod-per-step | a fresh pod each step | slowest | strongest | strict prod, untrusted steps |
| **warm-node / reusable-agent** | one long-lived node, steps run on it | fast | shared | cost-sensitive, trusted steps (the Jenkins model) |
| **DinD / rootless-podman** | containers inside a worker container | fast (warm) | medium | per-step container isolation without a new pod |
| **Dagger engine** | a reusable BuildKit engine; steps are containerised ops | fast + **cached** | medium | pipelines wanting node reuse *and* layer caching |

**Dagger specifically** gives the two asks in one thing: a **warm BuildKit engine** (no per-step pod
startup) plus a **content-addressed cache** (unchanged steps do not re-run). It runs as a DinD or
rootless-podman engine kept alive across runs.

## 3. The line that does not move

A reusable worker changes *where work runs*, never *whether it was governed*. Concretely:

- The governed saga runner still dispatches every step through `GovernedWorker` — a warm node does not get a
  raw execute path.
- A denied step still produces no side effect on the shared node.
- Reusing a node must not reuse *authority*: credentials are still brokered per action (see
  `credential-brokering.md`), so a warm agent does not accumulate standing access between steps. A long-lived
  node with long-lived credentials is the anti-pattern the broker exists to prevent.

## 4. Isolation is a floor decision, not only a performance one

Containers-in-containers needs elevated isolation — privileged DinD, or rootless with known caveats. In a
sovereign zone that is exactly what the sealed floor and the WASM sandbox exist to bound. So a reusable /
DinD worker has an **isolation profile that is policy**:

- Trusted steps (a build feeding a verify) may share a warm node — fast, cheap, acceptable.
- Untrusted step logic (a tool from the registry) runs **sandboxed** regardless of the node, or gets its own
  unit. Sandbox the untrusted thing; do not make the whole node privileged to accommodate it.

This mirrors the recording-level idea (`recording-levels.md`): performance/cost is a knob the deployment
turns; the floor — govern-before-execute, brokered credentials, sandboxed untrusted code — is not on the
knob.

## 5. What to build (when it is time — not now)

1. A `ReusableWorker` / `DaggerWorker` adapter behind `WorkerPort`, selected by config
   (`worker: pod | reusable | dagger`), holding a warm engine handle rather than spawning per step.
2. It dispatches only via `GovernedWorker` / `runUnder` — the same governed path as the pod worker.
3. Credentials leased per action from the broker, never cached on the warm node.
4. An isolation profile in floor data: which steps may share the node, which must sandbox or isolate.

**Not on today's path.** Today's order is: consolidate the Java stacks → governed saga runner + recording
levels. The reusable worker is a later adapter that plugs into that runner without changing it — captured
here so the boss's cost/flexibility ask has a designed home when it comes up.
