# Releasing MantleKeep

MantleKeep is not yet published. This is the deliberate release sequence — do NOT automate it into a
hook or CI trigger; a release must be a conscious act.

## Step 0 — Gates (all must be true before any release)
- [ ] **Ownership/authorship confirmed in writing** — the authors own this generic framework and may
      license/relicense it. Consult legal if unsure.
- [ ] **Final human review** — a person reads the diff for anything that should not be public.
      Automated scans miss vocabulary and context; do not skip the human pass.
- [ ] **License confirmed** — Apache-2.0 unless you decide otherwise.
- [ ] **Module path decided** — Step 1 (a one-way door).

## Step 1 — Decide the module path (ONE-TIME, before v0.1.0)
The code currently uses the vanity path `mantlekeep.dev/…` (Go) / `dev.mantlekeep` (Java).
- **Vanity (`mantlekeep.dev`):** prettier + future-proof, but you must own `mantlekeep.dev` and serve a
  `go-import` meta redirect for `go get` to resolve.
- **Host path (`github.com/<org>/mantlekeep/…`):** zero setup; the host serves it.
Pick ONE now. Changing it after v0.1.0 breaks every downstream consumer (a major-version bump).

## Step 2 — Publish (only once Step 0 is green)
1. Push to the public repo and make it public.
2. Finalize `CHANGELOG.md` — move the `[0.1.0]` block out of Unreleased and date it.
3. Tag: `git tag v0.1.0 && git push origin v0.1.0` (Go: the tag IS the module version).
4. Publish the Java artifacts to a public Maven repo as `dev.mantlekeep:…:0.1.0`.
5. Create the GitHub Release for `v0.1.0` with notes from the CHANGELOG.

**Before the first push, squash the local history into a single clean initial commit** — so the public
repo starts at v0.1.0 with no intermediate history.

## Ongoing releases
Each release: land the changes, bump `CHANGELOG.md`, tag, publish the artifact, cut a GitHub Release.
Keep the history clean and forward — never rewrite a published tag.
