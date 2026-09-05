# mantlekeep-oidc

Verifies an OIDC bearer token and resolves it to a caller. Standard library only — no
third-party dependencies.

```go
serve.Run(serve.Options{
    Ports:   []estate.Port{ /* your adapters */ },
    Callers: &oidc.VerifiedCallers{
        Issuer:   "https://idp.example.com",
        Audience: "mantlekeep-estate",
        Keys:     keys,   // from a JWKS document
    },
})
```

## Why this is a separate module

RS256 — the algorithm essentially every identity provider issues by default — is
**RSASSA-PKCS1-v1_5**, and a static analyser cannot tell it apart from PKCS#1 v1.5
**encryption**, which is genuinely broken.

| | status |
|---|---|
| PKCS#1 v1.5 **encryption** | broken — Bleichenbacher's attack |
| PKCS#1 v1.5 **signatures** | no practical break, NIST-approved. This is RS256 |

That distinction is correct and unpersuasive to a scanner. An organisation running its own
analysis on what it downloads would find a CRITICAL result on a module it did not choose to
take — and a dismissal in someone else's dashboard does not travel.

So `mantlekeep-estate` carries **no RSA verification at all** and scans clean. A deployment
that wants tokens verified takes this module deliberately, and takes the finding with it,
with the reason written where the finding appears.

Same rule as the Kubernetes adapter carrying `client-go`: the module that needs the weight
carries it, and nobody else pays.

**PS256 is preferred and supported.** RSASSA-PSS has a security proof PKCS1-v1_5 lacks. A
deployment whose issuer offers PS256 gets it; RS256 is retained because refusing it would
mean refusing real tokens from real identity providers.

## What it verifies

Signature **first**, then claims — claims from an unverified token are attacker-controlled
text, so reading `iss` or `exp` before checking the signature would be trusting the thing
being authenticated.

- **signature** against the issuer's public keys
- **algorithm** against an allow-list, never used to *select* one. `alg: none` and `alg: HS256`
  are both refused; honouring the algorithm a token names is how a token verifies itself
- **issuer**, **audience** — a token minted for another service by the same issuer is refused
- **expiry** — and a token with no `exp` is refused, because absent must not read as "never
  expires"

## What it deliberately does not do

- **never runs the OIDC login flow** — no redirect, no code exchange, **no client secret**.
  Verification needs only the issuer's *public* keys, which is what lets it work air-gapped:
  mirror the JWKS to a file, and a mirror of public keys is not a secret to protect
- **never reads roles from the token** — the claims say who the caller is; what that means is
  the deployment's own mapping, resolved server-side. A token that could assert its own roles
  could assert its way past any gate
