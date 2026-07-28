# WHAT: the tamper-evident audit chain (`internal/audit`)

**Plain-language walkthrough for a non-Go reviewer.** No Go knowledge required.

## Purpose (one paragraph)

This package is MantleKeep's **evidence trail**. Every governed decision (who did what, when,
allowed or denied) is written here as an append-only record, and each record carries a
**fingerprint (SHA-256 hash) that includes the fingerprint of the record before it**.
That chaining is what makes the log **tamper-evident**: if anyone edits, deletes, or
reorders a past record, the fingerprints stop matching and a one-line `Verify()` walk
detects it. Records are stored in a small embedded database file (`audit.db`, via
bbolt) that is **not wiped on restart**, so reopening the binary continues the same chain
— the trail is continuous across restarts. Today the storage is bbolt; the design leaves
room to move to ClickHouse or S3 WORM later without changing the chaining idea.

## Why hash-chaining makes it tamper-evident (the one idea to get)

Think of each record as a numbered page in a ledger where every page also copies down a
seal from the previous page:

- Each record's **Hash** is computed over its own contents **plus the previous record's
  Hash** (`PrevHash`).
- So Hash #5 depends on Hash #4, which depends on #3, ... all the way back to #1.
- Change anything in record #3 and its Hash changes → but record #4 still stores the
  *old* Hash of #3 → the link breaks → `Verify` flags it.
- You cannot "fix" it by editing #3 alone; you'd have to recompute #3, #4, #5 … the
  entire chain forward. That's the tamper-**evident** guarantee: not that editing is
  impossible, but that any edit is *detectable*.

## The key decisions, with file:line anchors

| Decision | Where | What it means |
|---|---|---|
| Storage is append-only, hash-chained | `audit.go:1-3`, `20-23` | Package doc states the contract; `Bolt` is the bbolt-backed implementation. |
| The fingerprint covers content + prior link, **not itself** | `audit.go:45-50` | `hashRecord` blanks the record's own `Hash` field before hashing, then hashes the rest (which includes `PrevHash`). Hashing must exclude the field you're about to store the hash into. |
| Writing a record links it to the last one | `audit.go:54-73` | On each `Log`: read the previous record, copy its Hash into this record's `PrevHash`, compute this record's Hash, append under the next sequence number. |
| Continuity across restarts | `audit.go:59-64`, wired at `app/door.go:31-34` | The previous hash is read from the stored last record, so reopening `audit.db` continues the existing chain rather than starting fresh. |
| **`Verify` walks the whole chain** | `audit.go:76-96` | Reads records in order, carrying the expected previous hash. For each record it checks **two** things: (1) its stored `PrevHash` equals the actual previous record's Hash, and (2) recomputing its Hash still matches its stored Hash. Any mismatch → `intact = false`. |
| Verdict is a single boolean | `audit.go:76-77`, `95` | `Verify` returns `true` = untampered, `false` = chain broken. |

## How to review this WITHOUT reading Go

1. **You only need to understand two functions.** `hashRecord` (`audit.go:45-50`) =
   "compute this record's fingerprint". `Verify` (`audit.go:76-96`) = "re-walk the chain
   and check every fingerprint still matches." Everything else is plumbing.
2. **Check `Verify` tests both links.** In the loop at `audit.go:87`, the condition is:
   *if the stored previous-hash doesn't match the real previous hash, OR recomputing the
   hash doesn't match what's stored → not intact.* Two independent checks means both
   "someone reordered records" and "someone edited a record's contents" are caught. Read
   that one `if` and you've verified the core guarantee.
3. **Confirm the hash excludes itself.** At `audit.go:46` the record's own `Hash` is
   cleared before hashing. If it didn't, the hash could never match on read-back. This is
   the one subtle correctness point — and it's a single line.
4. **Confirm it's not reset on boot.** The audit DB is opened as a durable file in
   `app/door.go:31-34` (comment: "NOT wiped on boot"), so the chain is continuous. This
   is why demo beat #4 ("durable evidence, restart, chain intact") works.
5. **Prove it by running the verifier.** The chain has a CLI check today: the bare
   `mantle` command opens the audit DB and prints `audit hash-chain intact: true`
   (`cmd/mantle/main.go:102-105`); the products demo prints the same for one shared chain
   across all products (`cmd/mantle/products.go:72-75`). Run it, tamper with `audit.db`
   by hand, run again — it will print `false`. That's the guarantee, demonstrated without
   reading any Go. **Known gap:** there is no HTTP endpoint for verification yet — it is
   CLI-only.
