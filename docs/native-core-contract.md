# The native core Decision contract — one shape, any language

The door's verdict is a **language-agnostic contract**, not a Go struct. Whatever
implements the embedded core behind the FFI boundary — the Go core built as a
`c-shared` library today, a Rust core tomorrow — must emit the **same rich Decision**,
so a product reading it through `EmbeddedDoorClient` cannot tell which language decided.

This is the migration model made concrete: **Go is the oracle; a Rust core is verified to
produce byte-identical Decisions against Go via parity fixtures.** The contract below is
what both must satisfy.

---

## 1. The embedded FFI surface (SACRED — five symbols)

The core is loaded in-process over a tiny C ABI (`PanamaMantlekeepCore`, composition-model
§4c). The surface is versioned with the core and never sprawls:

| Symbol | Shape | Returns |
|---|---|---|
| `mantle_door_new(policyPathsJson)` | JSON array of policy-document paths | opaque door handle |
| `mantle_door_submit_json(door, intentJson)` | the Intent contract (§2) | the **Decision contract (§3)** |
| `mantle_door_audit_json(door)` | — | JSON array of audit records |
| `mantle_door_verify(door)` | — | `1` intact, `0` tampered |
| `mantle_door_free` / `mantle_string_free` | handle / returned string | void |

Only `submit_json` returns a Decision; that return value is the contract this document pins.

## 2. What goes IN — the Intent (snake_case)

`submit_json` receives the intent as JSON. The FFI boundary is snake_case throughout (it is
the same convention `door.md`'s `PolicyInput` and the audit records use):

```json
{
  "id": "JINT-001",
  "subject_id": "lead-bob",
  "action": "job.promote",
  "resource": "project/demo",
  "goal": "ship release 1.2",
  "params": { "env": "PROD" }
}
```

## 3. What comes OUT — the Decision (snake_case) — THE CONTRACT

`submit_json` returns exactly this shape. Three outcomes; fields present per outcome as noted:

```json
{
  "outcome": "allow" | "deny" | "require_approval",
  "token": "TOK-1",                       // present and non-empty on allow; "" otherwise
  "policy_id": "policy.sdlc.v3",          // which policy decided; "" if none loaded
  "reasons": [                            // typed reasons; [] on a clean allow
    { "code": "DENY_ACTION_NOT_ALLOWED", "message": "no role permits job.run" }
  ],
  "required_approvers": ["L4-Approver"],  // present on require_approval; [] otherwise
  "expires_at": "2026-08-01T00:00:00Z"    // RFC3339 token validity on allow; "" otherwise
}
```

**Casing is deliberate.** The **native/FFI boundary is snake_case**; the **HTTP wire is
camelCase**. Same logical contract, one convention per boundary — each boundary is
internally consistent with its neighbours (FFI intent/audit are snake; the web wire and its
JS/Java clients are camel). The fields map one-to-one:

| Logical field | Native (FFI, snake) | HTTP wire (camel) | Notes |
|---|---|---|---|
| outcome | `outcome` | `outcome` | `allow` / `deny` / `require_approval` |
| token | `token` | `token` | on allow |
| policy id | `policy_id` | `policyId` | which policy decided |
| reasons | `reasons[].code` / `.message` | `reasons[].code` / `.message` | identical nested shape |
| required approvers | `required_approvers` | `requiredApprovers` | on require_approval |
| token expiry | `expires_at` | `expiresAt` | RFC3339 |

### Typed reason codes (stable)

The `code` is the stable, queryable classification an auditor switches on without parsing
prose: `DENY_FLOOR`, `DENY_SEPARATION_OF_DUTIES`, `DENY_IDENTITY`,
`DENY_ACTION_NOT_ALLOWED`, `DENY_VALIDATION`, `DENY_POLICY_ERROR`, and `REQUIRE_APPROVAL`.
Unrecognised or missing `outcome` **fails closed to deny** (see `Decision.Outcome.fromWire`).

## 4. Go is the oracle; Rust matches Go

There is no Go `c-shared` build target today — this document pins the contract so that when
one is built (Go first, as the reference), and a Rust core follows, both are held to the
identical shape:

1. **The Go core is the reference implementation.** Its `/api/govern` handler already emits
   the camelCase form of this contract; `mantlekeep-control/doorserver/govern_wire_test.go`
   pins it. A Go `c-shared` `submit_json` re-serialises the *same* Decision in snake_case.
2. **A Rust core is parity-gated.** It is accepted only when it produces **byte-identical**
   Decisions to Go for a shared fixture set (the governed-dev experiment method: experiment
   in the cheapest language, lock fixtures, translate to Rust gated by parity — Go stays the
   oracle, possibly forever). The rich 3-state Decision is now part of that Rust parity
   target, alongside the audit-chain and verify behaviours proven in the rust-core spike.
3. **The reference double lives in the tests.** `InMemoryNativeCore`
   (`sdks/java/mantlekeep-door-client/src/test`) emits this exact snake_case contract, and
   `EmbeddedDoorClientTest` decodes all three outcomes off it — so the Java embedded path is
   proven against the real contract with no native library present. Any native binding
   (Panama over Go-c-shared or Rust) must satisfy the same tests.

## 5. Changing the contract

Adding or renaming a field, or removing a code, is a **breaking change**: it must bump the
core contract version (`ContractVersion` in `contracts.go`, in lockstep with the wire) and
update the fixtures on both sides. The whole point of this document is that the change is
made **once**, against one contract, and both language cores are re-verified against it.
