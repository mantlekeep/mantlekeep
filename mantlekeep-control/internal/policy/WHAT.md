# WHAT: the policy engine (`internal/policy`)

**Plain-language walkthrough for a non-Go reviewer.** No Go knowledge required.

## Purpose (one paragraph)

This package is MantleKeep's **law**. It is the single place that answers one question:
*"Is this subject allowed to do this action, in this environment, right now?"* It does
so with pure Go and (almost) no third-party libraries, so the core binary stays lean
and has a tiny security-scan surface. It combines four ideas: a **role→action table**
(RBAC — who may do what), a **promote gate** (who may push to which environment — DEV,
SIT, UAT, PROD), a **sealed cascade** (a host/platform floor that teams may tighten but
never loosen), and two hard **guardrails** — AI agents can never approve, and no one can
approve their own request (separation of duties). A **failsafe** wrapper sits in front:
if the engine ever errors, the door degrades to read-only (allow reads, deny every
write) rather than fail open.

## The key decisions, with file:line anchors

| Decision | Where | What it means |
|---|---|---|
| Role → allowed actions table | `rbac.go:22-28` | `L0-SuperAdmin` = everything (`"*"`); each lower role gets an explicit shorter list; `AI-Agent` gets only `logs.read` + `mr.propose`. |
| Role seniority ranking | `rbac.go:49-51` | Lower number = more senior. `L0=0, L1=1, L2=2, L3=3, AI-Agent=4`. A subject may do an action if it holds a role at least as senior as the action requires. |
| Env-gating is DATA, not engine code | `floor.go` (`required_role_when`) + a product's `floors.json` | The engine hardcodes NO promote gate and NO environment. An env-gated action (e.g. a product's *promote*) is a normal grant PLUS a `required_role_when` floor rule in the product's floor DATA — e.g. a product gating its `image.promote` action to prod. |
| The main verdict function | `rbac.go:92-125` | Reads a typed request (subject, action, goal, env, requester) and returns allow/deny. This is the whole decision in ~30 lines. |
| Guardrail — goal required | `rbac.go:101-103` | Every request must state a `goal`; a blank goal is denied. Forces intent to be logged. |
| Guardrail — **AI cannot approve** | `rbac.go:104-106` | If the subject is an AI *and* the action is `mr.approve`, deny — full stop, before any role check. |
| Guardrail — **separation of duties** | `rbac.go:120-123` | If a `requester` is supplied and equals the acting subject, deny — you cannot approve your own request. |
| Config-authored products carry their own authz | `rbac.go:58-60`, `141-155` | A Canvas/manifest product declares a `RunAs` role; the engine consults that when the static table doesn't know the action. Adds products with **no code change**. |
| Layered config reaches the law | `rbac.go:83-89` | `WithResolved` feeds the cascaded config (below) into the engine. |

### The sealed cascade (the anti-hack part) — `resolve.go`

| Decision | Where | What it means |
|---|---|---|
| Cascade order | `resolve.go:43-64` | Layers apply least-specific first: **MantleKeep default → platform (host) → product → team**. For a normal key, the most-specific layer wins (like Helm/Spring profiles). |
| The seal rule | `resolve.go:68-75` | If a key is **sealed** by an upper layer, a lower layer's override is accepted **only if it is at least as strict** (a more senior role). A looser or missing override is silently rejected — the floor holds. |
| Seals recorded *after* own values | `resolve.go:57-62` | The layer that sets a floor sets it freely; only the layers *below* it are constrained. This is why the host can seal `promote:PROD` and the team beneath it cannot loosen it. |
| "At least as senior" test | `resolve.go:109-116` | An **unknown** role never counts as senior enough, so a typo or bogus role can never sneak a floor looser. |
| Default layer is unsealed | `resolve.go:98-104` | MantleKeep's own baseline seals nothing, so teams are free by default; the host adds the floor on top. |

### The failsafe wrapper — `failsafe.go`

| Decision | Where | What it means |
|---|---|---|
| Fail safe, never open | `failsafe.go:43-54` | If the real engine **errors**, or is deliberately tripped, fall back to a compiled-in read-only policy instead of erroring or allowing. |
| Read-only fallback | `failsafe.go:57-72` | Reads allowed, every write denied. |
| What counts as a read | `failsafe.go:75-85` | Action suffixes `.read/.view/.list/.status/.get`. Anything else (including blank) is treated as a write and denied. |

## How to review this WITHOUT reading Go

1. **Read the map at `rbac.go:22-28` as a table.** Left column = role, right = the
   actions it may perform. `"*"` means "all". Ask: *does this match who should be able
   to do what?* That is 80% of the policy.
2. **Check the two guardrails are unconditional and come first.** At `rbac.go:104` the
   AI-approve denial happens before any role lookup — an AI can never talk its way past
   it. At `rbac.go:120` the self-approval denial is likewise a flat comparison of two
   names. If those two lines are present and early, the guardrails are intact.
3. **Confirm the promote gate reads the env as a parameter, not a hardcoded rule.**
   `rbac.go:109-118` looks the env up in a table; the table itself is `rbac.go:34-37`
   (default) or supplied by config. So "who may ship to PROD" is a config value, not
   buried logic.
4. **The seal rule is one `if`.** `resolve.go:68-75`: *if the key is sealed and the new
   value is not at least as strict, do nothing (keep the floor).* Read it as: "a team can
   only make a host rule tighter, never looser." That single function is the entire
   anti-hack guarantee.
5. **Drive it instead of reading it.** The tests in this folder assert every rule in
   plain terms — `rbac_test.go` (AI-cannot-approve, promote gate, self-approve) and
   `resolve_test.go` (the sealed floor holds when a team tries to loosen PROD). Run
   `cd mantlekeep-control && go test ./internal/policy/` and read the test **names** — they
   are English statements of each guarantee. To see it live, follow the demo curl in the
   task brief: log in as `lead-bob` (Operator) and POST `/api/govern` with
   `env:"PROD"` — you get `deny`; as `arch-carol` (Architect) you get `allow`.
