# Identity: how a caller is established, and what is deferred

## What ships

The estate does **not** verify tokens. It resolves the caller through
`serve.Options.Callers` — an injected `api.CallerResolver` — and when nothing is supplied it
reads a header set by whatever authenticates in front of it.

```
person → gateway (oauth2-proxy / IAP / Keycloak / Dex) → estate
                  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
                  verifies the OIDC token, sets X-Caller,
                  and strips any client-supplied copy
```

The trusted-header tier is **fenced**: it works freely on loopback, and off loopback it is
refused unless `MANTLEKEEP_TRUST_HEADER=true` names the decision. So a laptop, a demo and an
air-gapped first day all work with no identity provider, and none of them silently becomes
production.

### The hole this leaves, and how it is closed

A **port-forward** reaches the service directly, bypassing the gateway — and then the header
is whatever the caller typed. For a read that is small; for an approval it is separation of
duties defeated by curl.

It is closed at the network, not in the process:

```yaml
# only the gateway may reach the estate
kind: NetworkPolicy
spec:
  podSelector: { matchLabels: { app: mantlekeep-estate } }
  ingress:
    - from:
        - podSelector: { matchLabels: { app: your-gateway } }
```

That is a hardening control a platform team already understands, and it removes the path
rather than detecting its abuse.

## What is deferred, and why it is written down

In-process verification was **built and then removed**. It verified a bearer token against the
issuer's public keys — signature first, then issuer, audience and expiry; algorithm checked
against an allow-list so `alg: none` and `alg: HS256` were both refused; never running the
OIDC flow and therefore holding no client secret.

It was removed for one reason, and it is not a reason about correctness.

RS256 — what essentially every identity provider issues by default — is **RSASSA-PKCS1-v1_5**.
A static analyser cannot tell that apart from PKCS#1 v1.5 **encryption**, which is genuinely
broken (Bleichenbacher). The signature scheme has no practical break and is NIST-approved.

| | status |
|---|---|
| PKCS#1 v1.5 **encryption** | broken |
| PKCS#1 v1.5 **signatures** — this is RS256 | no practical break, NIST-approved |

That distinction is correct and unpersuasive to a scanner. An organisation that scans this
repository before adopting it would see a CRITICAL finding, and a dismissal in our dashboard
does not travel to theirs. Being scannable is a feature of a governance framework: a tool that
cannot pass the review it exists to support is not usable, however sound its cryptography.

### Bringing it back

The seam is already in place — `serve.Options.Callers` takes any resolver, so verification
returns without touching this module. Three ways, in increasing order of what they cost:

1. **PS256 only.** RSASSA-PSS has a security proof PKCS1-v1_5 lacks, and it raises no finding.
   Viable where the issuer can be configured to issue PS256 (Keycloak can; several cannot).
2. **A separate repository.** Verification with both algorithms, scanned on its own terms, with
   this reasoning where the finding appears. Deployments choose it deliberately.
3. **Both, in this repository**, once the rule distinguishes signatures from encryption.

The removed implementation is preserved rather than rewritten from scratch — see the
deferred-work directory referenced in the project state document.

### Why this is recorded rather than simply dropped

Defence in depth is not an anti-pattern, and verifying at the door as well as at the gateway
would be strictly better: it stops depending on the network path being what it was designed to
be. The decision here is not that in-process verification is wrong — it is that a clean scan
buys adoption today, and the capability can return without a redesign.

Writing that down is itself the point. A control that was considered, built and deliberately
deferred is evidence of a decision; one that was never mentioned is evidence of an oversight.
