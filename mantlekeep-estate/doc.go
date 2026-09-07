// Package estate turns a team's declared footprint into governed grants across the footprint.
//
// A team declares WHAT it needs in a manifest — topics, schemas, projects. The FLOOR, which
// lives in server config, decides HOW MUCH: quotas, retention, connection limits, expiry.
// The manifest cannot name a limit; the schema has no such field and the decoder rejects
// unknown ones, so an attempt to widen a limit fails at parse time rather than being quietly
// ignored. Config chooses the policy; it can never reach the guarantee.
//
// The unit of governance is a CHANGE, not a manifest. Provisioning a playground topic inside
// a namespace the team already owns is instant; taking a new namespace, or raising something
// to production, is gated — and the gate is chosen by CONSEQUENCE rather than by asset. Gating
// every change equally is what makes a golden path slower than the bypass, and a bypassed
// guardrail governs nothing.
//
// This package DECIDES. It never talks to Kafka, Postgres or Harbor itself: backends live
// behind [Port], one adapter per asset, so the decision logic never learns what a broker is.
//
// # Extending it without forking it
//
// Two seams exist for a deployment this module cannot see, and neither of them teaches it
// anything about that deployment:
//
//   - [Labels] carry the deployment's OWN grouping vocabulary through to every resolved
//     [DesiredItem]. Descriptive only: nothing here reads one to decide, and a label may not
//     take the name of a field that is read to decide.
//   - [ChangeTransformer] lets a change be rewritten BEFORE it is submitted to the door, so what
//     a person approves is what will actually be applied. Optional — unset means no call at all.
//
// Both are additive and both are configured, never edited. A deployment that uses neither
// behaves exactly as it did before they existed.
//
// # Why JSON rather than YAML
//
// Manifests are authored in YAML because humans read it, but this package parses JSON. The
// conversion belongs at the edge (portal, CLI), which keeps a YAML parser off this module's
// dependency list — the same discipline that keeps the core linking only bbolt. JSON is a
// subset of YAML, so nothing is lost but the comments.
package estate
