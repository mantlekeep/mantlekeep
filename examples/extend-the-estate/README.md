# Extending MantleKeep without forking it

This directory is a complete, compiling example of how an organisation deploys MantleKeep as
**its own product** — its name, its environment prefix, its adapters — while the framework stays
an unmodified dependency pinned in `go.sum`.

    go build ./...        # against the published modules, nothing local
    go run .

## The rule

**Wrap and extend. Never copy.**

A copy compiles on the day it is made and then diverges. Every fix afterwards has to be made
twice, and the second one is the one that gets forgotten. Nothing in this example is copied from
upstream, so nothing in it can drift from upstream; upgrading the framework is a version bump.

## The three things an organisation wants, and where each comes from

### 1. Its own name and environment prefix — `app.Brand`

```go
app.Brand(app.BrandOptions{
    Prefix: "ACME", Name: "Acme Control", Mark: "▲",
    Kicker: "governed delivery", Tagline: "every action through one door",
})
```

Operators then speak `ACME_ESTATE_ADDR`, `ACME_DOOR_URL`, `ACME_AUTH` — the whole surface — and
the framework reads its own prefix underneath. The brand is **configuration, never compiled in**:
`ACME_BRAND_NAME` overrides the default at runtime.

The remap fills only variables the operator left **empty**, so setting the framework's own
variable directly still wins. That matters when somebody is debugging at three in the morning and
needs to override one value without learning the whole mapping.

### 2. Its own adapters — implement `estate.Approved`, wrap with `estate.Guarded`

The framework knows the **port**; your binary knows the **backend**. It never learns what your
infrastructure is.

Implement `Approved` rather than `Port`, and wrap it:

```go
serve.Run(serve.Options{
    Ports: []estate.Port{estate.Guarded(&deploymentAdapter{})},
})
```

`Guarded` runs the refusals every adapter owes — an empty token, an expired token, a change for
another asset, a kind this adapter does not handle — **before your backend is touched**. Writing
those four checks yourself in four adapters is four places to forget the fifth, and they are not
convenience code: they are what makes *govern before execute* true at the edge.

Two adapters can share one asset. Declare `Kinds()` and compose them with `estate.ByKind`; two
adapters claiming one kind is refused **when they are wired**, not when a change arrives. An
ambiguity left to map iteration order is the same change reaching a different backend on
different days, with nothing in the record to explain it.

### 3. Its own identity source — `serve.Options.Callers`

Present in the estate's current source, not in the `v0.1.0` tag this example pins, so it is not
shown above. An organisation supplies a resolver from **its own module** — which is why the
framework carries no crypto: nobody scanning an estate they adopted for governance should find an
RSA finding inside it.

Until then, identity comes from the trusted-header tier, which is fenced to loopback unless a
deployment explicitly names the decision.

## Two things about the execution token

An `ExecutionToken` carries two different things and confusing them has cost people an incident:

- **`token.Value`** is the opaque signed capability it authorises with. Never write it onto an
  object, into a label, or into a log line.
- **`token.IntentID`** is the chain reference. **Record this** on whatever you create, so a live
  resource can be traced back to the decision that allowed it.

## Report reality, never intent

`Observe` must report what is **really there**, read back from the backend. An adapter that
echoes what it was told to do makes every drift report say *no drift* — which is worse than
having no report, because people believe it.

## What is deliberately NOT here

No plugin registry, no discovery mechanism, no configuration naming a Go type. The binary that
knows both the framework and the backend is the one that wires them together, because it is the
only thing that legitimately knows both. That binary is yours, it is about thirty lines, and it
is the entire extension surface.
