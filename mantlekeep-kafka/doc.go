// Package kafkagrant applies an APPROVED grant to an Apache Kafka cluster.
//
// It is an ADAPTER, not a door. It decides nothing: by the time a caller reaches this
// package the door has already allowed the intent, and this package's only job is to
// make the cluster match what was approved — and then to report what the cluster
// actually says, not what the request asked for.
//
// # Two operations, deliberately asymmetric
//
// The asymmetry is the design. Onboarding and provisioning are not two sizes of the
// same act:
//
//	OnboardTeam(Boundary) — RARE, gated. Gives a team a NAMESPACE it owns: PREFIXED
//	ACLs over a topic/group prefix, plus a byte-rate quota for its principal. This is
//	the act a human approves.
//
//	Provision(Grant) — FREQUENT, instant. Creates ONE topic INSIDE a namespace the team
//	already owns. It grants no new permission: the prefix ACL from onboarding already
//	covers the topic. Nothing here needs a fresh approval, so nothing here waits for one.
//
// Collapsing the two — a LITERAL ACL per topic — is the failure mode this package
// exists to avoid. It recreates ACL sprawl, and it makes every throwaway playground
// topic a governance event. A golden path that is slower than the bypass stops being
// used, and a governance model nobody uses governs nothing.
//
// # What this package refuses to do
//
//   - It never grants CREATE on the prefix. See [TopicOperations] for why.
//   - It never invents a limit. Quotas, retention, partitions and replication factor
//     are INPUTS. Deciding them is the caller's floor, not this adapter's business.
//   - It never reports a result from its own request. Every artifact is read back from
//     the cluster (DescribeACLs / DescribeClientQuotas / DescribeTopicConfigs). A result
//     echoed from its input is testimony, not evidence.
//   - It never widens a boundary. A resource outside the granted prefix is refused here
//     even though the caller is expected to check too — defence in depth, so a bug
//     upstream cannot become a permission nobody approved.
//
// # Dependency direction
//
// This module depends on the core (github.com/mantlekeep/mantlekeep/mantlekeep-control)
// as a library, one way, never back. It is a SEPARATE Go module precisely so the Kafka
// client never becomes a dependency of the core: a CVE — or a registry quarantine — in
// the Kafka tree must not be able to block the core's build. The core links only bbolt.
//
// # Testing
//
// The cluster is reached through the [Admin] interface, so every decision in this
// package is unit-testable with a fake. No test in this module needs a live broker.
package kafkagrant
