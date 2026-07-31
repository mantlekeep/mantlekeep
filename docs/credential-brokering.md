# Credential brokering — making governance structural

**The problem, stated plainly.** A service that holds both a door client and the means to act can simply
not call the door. Nothing in the process stops it, and adding a check inside that process does not help:
whoever can add code that skips the door can delete the check too.

So governance enforced **inside** the executing process is *advisory*. It gives you a decision point and a
tamper-evident record of everything that was asked — which is worth a great deal — but it is not proof that
nothing else happened.

**What structural enforcement means:** bypassing leaves you **unable to act**. Not "you skipped the guard"
but "you have nothing to execute with."

---

## 1. The ladder, so the trade-off is explicit

| | Control | Bypassed by | Nature |
|---|---|---|---|
| 1 | convention + code review | writing three lines | social |
| 2 | an architecture test — only the manager may reference the worker | deleting the test | catches accidents |
| 3 | the worker requires a token | deleting the check | same process, same author |
| 4 | the token is **signed** by the door; the worker verifies | forging a signature | binds only components that verify |
| 5 | **credential brokering** — the executor holds no credentials until the door releases them | **nothing in-process** | structural |

Levels 2–4 raise the cost of bypassing. **Only level 5 removes the capability.** Build 2 today because it
is an hour and catches the accidental case; build 5 because it is the one that is true.

## 2. The design

The executing service **never holds a long-lived credential**. It holds only the ability to ask.

```
   session service                     the door (per zone)            the cluster
   ───────────────                     ───────────────────            ───────────
   submit(intent)  ─────────────────▶  decide · record
                                       on ALLOW: mint a
                   ◀─────────────────  SHORT-LIVED credential
                                       scoped to this intent
   run helm with it  ─────────────────────────────────────────────▶  authenticates
                                                                      (and expires)
```

Properties that make it work:

- **Scoped to the decision.** The credential permits the namespace, cluster and verbs the intent named —
  nothing else. A token approved for `deploy` in one namespace cannot delete another.
- **Short-lived.** Minutes, not hours: long enough for the effect, too short to bank for later. Expiry is
  the difference between a credential and a key.
- **Never at rest in the service.** Held in memory for the duration of the work, never written to disk or
  configuration. A pod dump reveals at most one in-flight credential.
- **Bound to the audit record.** The credential's identifier appears on the chain entry, so a cluster-side
  log can be joined to the decision that authorised it. That join is what makes the whole thing provable
  rather than merely orderly.

**The bypasser's position afterwards:** they can call helm all they like, against a cluster that will not
authenticate them. There is no configuration to read and no secret to steal, because the service was never
given one.

## 3. Air-gapped zones — the broker follows the door

An isolated zone has no reachback, so a central broker is not an option. **The rule is the same one that
governs the chain: one door per zone, and therefore one broker per zone.**

```
   CONNECTED ZONE                          AIR-GAPPED ZONE
   ┌───────────────────────┐               ┌───────────────────────┐
   │ door + broker         │               │ door + broker         │  ← its own, local
   │ holds this zone's     │               │ holds THIS zone's     │
   │ credentials           │               │ credentials           │
   │        ↓ short-lived  │               │        ↓ short-lived  │
   │ service → cluster     │               │ service → cluster     │
   └───────────────────────┘               └───────────────────────┘
            no credential ever crosses the boundary
```

- **A zone's credentials never leave it.** Not to a central broker, not in a backup, not in a config
  repository. This is the same discipline as the per-zone chain, applied to secrets — and it is what
  residency requires anyway.
- **The zone brokers offline.** Its door decides locally and mints locally. If the link to anywhere else is
  cut, the zone continues to govern *and* to execute, because nothing it needs is elsewhere.
- **A cross-zone dispatch does not carry a credential.** The far zone's door decides for itself and mints
  its own (see `multi-zone.md`). What crosses the boundary is a request and, coming back, a result hash —
  never the means to act.

**If a zone cannot broker locally, it is not air-gapped** — it is a remote zone with a dependency, and it
will fail closed the moment the link does. That is the test to apply to any deployment claiming isolation.

## 4. Agent workers — the case that most needs this

An AI agent executing work is exactly where advisory governance is weakest: the agent produces code and
commands, and an agent that can write code can write code that skips a check. The same reasoning as §1,
with a shorter path from "could" to "did".

So an agent worker gets the same treatment, tightened:

- **No standing credential, ever.** The agent asks the door per action and receives a credential scoped to
  that action alone. There is nothing in its environment to reuse.
- **Shorter lifetimes than a human's.** An agent acts in seconds; a credential outliving the action is
  latent capability that nobody is watching.
- **The action, not the session, is the unit.** A credential minted for "deploy this session" does not also
  permit "delete that one" — otherwise one approval becomes a general licence, which is how an agent's
  blast radius silently becomes the whole cluster.
- **The sealed floor still applies underneath.** An agent cannot approve its own work, so it cannot mint
  itself a credential by approving its own request. Brokering does not create a new path around that; it
  inherits it.
- **Untrusted step logic runs sandboxed** regardless (`behaviours.md` §8). Brokering controls what an agent
  may *reach*; the sandbox controls what its code may *do* locally. Both, not either.

## 5. What this does and does not give you

**Does:** an executor that cannot act without a decision, credentials that expire before they can be
hoarded, a cluster-side log joinable to the decision that authorised it, and a zone that keeps all of this
true while isolated.

**Does not:** stop someone who already holds cluster credentials by another route. Brokering governs the
path it owns; it does not repair an estate where operators hold standing admin. **Say this plainly to
anyone evaluating it** — a control oversold is worse than one honestly scoped, because it stops people
looking for the gap it does not cover.

## 6. Build order

1. **The architecture test** (level 2) — an hour, and it makes the intended path explicit in CI rather than
   in reviewers' memories.
2. **A `CredentialBroker` port** with a development implementation that returns the existing static
   credential. No behaviour change; it moves the seam into place.
3. **One real backend** — short-lived cluster credentials for a single action, proving scope and expiry.
4. **Bind the credential id onto the audit record**, so the join to cluster-side logs exists from the start
   rather than being retrofitted.
5. **Remove the static credential from the service's configuration**, which is the step that makes the
   guarantee real. Until this is done, everything above is preparation.
