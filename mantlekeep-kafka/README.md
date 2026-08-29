# mantlekeep-kafka — the governed-grant adapter for Apache Kafka

This module applies an **approved** grant to a Kafka cluster through the Admin API. It
decides nothing. By the time a caller reaches it the door has already allowed the intent;
its job is to make the cluster match what was approved, then report what the cluster
**actually says**.

It is a **separate Go module** on purpose. The core (`mantlekeep-control`) links only
bbolt; every adapter carries its own heavy client dependency, so a CVE — or a registry
quarantine — in the Kafka tree can never block the core's build. The dependency runs one
way: `mantlekeep-kafka → mantlekeep-control`, never back.

## Two operations, deliberately asymmetric

The asymmetry is the design. These are not two sizes of the same act.

| | `OnboardTeam(boundary)` | `Provision(grant)` |
|---|---|---|
| How often | **Rare** | **Frequent** |
| Gated? | Yes — a human approves it | No wait; nothing new is granted |
| What it does | Gives a team a **namespace it owns** | Creates **one topic** inside a namespace it already owns |
| ACLs written | `PREFIXED` over the prefix — TOPIC: `READ`, `WRITE`, `DESCRIBE`; GROUP: `READ` | none |
| Also | a producer/consumer **byte-rate quota** for the principal | applies the retention floor passed in |

**`CREATE` is deliberately not granted.** The team may read and write everything under its
prefix and still cannot bring a topic into existence. Topic creation stays a governed act,
routed through `Provision`, so the cluster's topic inventory is something that was decided
rather than something that accumulated. Granting `CREATE` on the prefix would look
identical on day one and would permanently mean the inventory is whatever every client
library's auto-create happened to produce.

**`PREFIXED`, never `LITERAL`.** A literal ACL per topic recreates the ACL sprawl this
model exists to escape, and makes every playground topic a governance event — which is how
a golden path becomes slower than the bypass and stops being used. A governance model
nobody uses governs nothing.

**The quota is not optional.** Without it one team can saturate the brokers for everyone —
the accidental denial-of-service this exists to prevent. It is also not *defaulted*: the
caller supplies both byte rates, because deciding a limit is the caller's floor, not this
adapter's business. The same goes for retention, partitions and replication factor.

## The rules it enforces

- **Refuse outside the prefix.** `Provision` rejects a resource name the granted prefix
  does not cover, before touching the cluster. The caller is expected to check too — this
  is the second check, so a bug upstream cannot become a permission nobody approved.
- **Idempotent.** A topic that already exists is **success**, not failure; a replayed grant
  converges. It does *not* rewrite an existing topic's retention — shortening retention
  deletes records, which deserves its own approval rather than being a side effect of a
  re-run. A divergence is reported in the artifact instead.
- **Read back, never echo.** Every artifact is built from `DescribeACLs`,
  `DescribeClientQuotas` and `DescribeTopicConfigs`. A result reported from its own input is
  testimony, not evidence — it says "success" in exactly the case you least want to be
  believed. `BoundaryArtifact.GrantsCreate()` exists for this reason: it reports a `CREATE`
  binding *this adapter never wrote*.
- **Bound before permitting.** `OnboardTeam` applies the quota **before** the ACLs, so
  there is no window in which a principal can produce before it can be throttled.

## Using it

```go
client, err := kgo.NewClient(kgo.SeedBrokers(seeds...)) // plus TLS / SASL
admin   := franz.New(kadm.NewClient(client))
adapter := kafkagrant.NewAdapter(admin)

token, err := door.Submit(ctx, intent)   // govern BEFORE execute
if err != nil {
    return err                            // a deny aborts before any side effect
}

artifact, err := adapter.OnboardTeam(ctx, token, kafkagrant.Boundary{
    Principal: "User:svc-payments",
    Prefix:    "payments.",
    Quota:     kafkagrant.Quota{ProducerByteRate: 10 << 20, ConsumerByteRate: 20 << 20},
})
```

Afterwards a topic is instant, and grants nothing new:

```go
artifact, err := adapter.Provision(ctx, token, kafkagrant.Grant{
    Principal:         "User:svc-payments",
    Prefix:            "payments.",
    Topic:             "payments.settlement.v1",
    Partitions:        6,
    ReplicationFactor: 3,
    RetentionMillis:   604800000,
})
```

### What the execution token does and does not do

Both calls take a `mantlekeep.ExecutionToken` and refuse an expired one. Be clear about
what that buys: the core states that a token is **unsigned** and is *evidence of a
decision*, not a capability. This adapter cannot verify that a token it was handed came
from a door, so the check does not make an ungoverned apply impossible. What it does is
stop an **expired** approval applying, and put the intent and policy that authorised the
work onto the artifact. The structural control is elsewhere — an executor that holds no
cluster credentials until the door releases them. See `docs/credential-brokering.md`.

## Layout

| File | |
|---|---|
| `doc.go` | the package doc — the design, in one place |
| `grant.go` | `Principal`, `Quota`, `Boundary`, `Grant` — the inputs, and their validation |
| `boundary.go` | prefix validation and `covers` — the check that mirrors `PREFIXED` exactly |
| `plan.go` | the operation sets, the quota alteration, the topic spec — **where `CREATE`'s absence lives** |
| `artifact.go` | `BoundaryArtifact`, `TopicArtifact` — the read-back evidence |
| `admin.go` | the `Admin` seam (`ACLAdmin` + `QuotaAdmin` + `TopicAdmin`) and the sentinel errors |
| `adapter.go` | the two operations |
| `franz/` | the only package that talks to a broker — franz-go `kadm` behind `Admin` |

## Build and test

```bash
cd mantlekeep-kafka
go build ./...
go test ./...
```

**No test in this module needs a live broker.** The cluster is reached through the `Admin`
interface, so every rule is exercised against a fake — including the already-exists
mapping, which is asserted against Kafka's real `TOPIC_ALREADY_EXISTS` error value rather
than a stub. What that means honestly: the *decisions* are proven; the *wire behaviour of
the franz-go calls against a real cluster* is not covered here and needs an integration
run against a broker.

## Dependencies

Pinned, and few:

```
github.com/twmb/franz-go/pkg/kadm  v1.18.0   admin API
github.com/twmb/franz-go          v1.21.0   kerr — the broker error vocabulary
github.com/twmb/franz-go/pkg/kmsg v1.13.1   the protocol enums (zero dependencies of its own)
github.com/mantlekeep/mantlekeep/mantlekeep-control  the core, one way
```

`kmsg` is used rather than a hand-rolled enum so a test asserting "PREFIXED, not LITERAL"
asserts the value that actually goes on the wire.
