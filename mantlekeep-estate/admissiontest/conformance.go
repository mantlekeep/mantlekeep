// Package admissiontest is the conformance suite every Admissions implementation must pass.
//
// The Admissions contract carries rules a type system cannot express. Admit "must refuse, with
// ErrAlreadyAdmitted, to overwrite a record that is currently admitted"; Get "must report a
// lapsed admission as expired, not admitted"; Revoke must replace rather than delete. A comment
// cannot enforce any of those, and a store that quietly breaks one is indistinguishable from a
// store that honours it — until an app is running in an environment nobody onboarded it to and
// the only record of who authorised it is whichever write landed last.
//
// So the rules ship as a runnable suite. A new store — Postgres, etcd, a config map — imports
// this and PROVES them, rather than being reviewed for them.
package admissiontest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// The messages used in more than one case, so each string exists once rather than in every case
// that happens to admit something.
const (
	wrapAdmitting = "admitting: %v"
	wrapReading   = "reading back: %v"
)

// Factory builds a fresh, empty store. Called once per sub-test so cases cannot leak into each
// other — a suite whose cases share state passes for reasons nobody can name.
type Factory func(t *testing.T) estate.Admissions

// Run executes the whole suite against one implementation.
// Named because the same sentence is asserted in several cases below.
const litRevoking = "revoking: %v"

func Run(t *testing.T, newStore Factory) {
	t.Helper()
	t.Run("an admitted app reads back as admitted", func(t *testing.T) { readsBack(t, newStore) })
	t.Run("an app nobody onboarded is not admitted", func(t *testing.T) { unknownIsRefused(t, newStore) })
	t.Run("only ONE admission wins under concurrency", func(t *testing.T) { onlyOneAdmitWins(t, newStore) })
	t.Run("a live admission is never overwritten", func(t *testing.T) { noSilentReadmission(t, newStore) })
	t.Run("revoking what is not admitted is refused", func(t *testing.T) { nothingToRevoke(t, newStore) })
	t.Run("a revoked app is no longer admitted and says why", func(t *testing.T) { revokedSaysWhy(t, newStore) })
	t.Run("a revoked app can be onboarded again", func(t *testing.T) { reAdmissionIsAllowed(t, newStore) })
	t.Run("a lapsed admission does not read as admitted", func(t *testing.T) { expiryIsHonoured(t, newStore) })
	t.Run("the environment is an opaque string", func(t *testing.T) { envIsOpaque(t, newStore) })
	t.Run("an unattributable admission is refused", func(t *testing.T) { attributionIsRequired(t, newStore) })
	t.Run("what is admitted in one environment can be listed", func(t *testing.T) { listsOneEnv(t, newStore) })
}

// admitted builds a complete, attributable admission — the shape a store is allowed to accept.
func admitted(team, app, env string) estate.Admission {
	return estate.Admission{
		Team: team, App: app, Env: env, State: estate.AdmissionAdmitted,
		Reason:    "onboarded with the platform team for the " + env + " footprint",
		Reference: "CHG-" + env + "-4471",
		DecidedBy: "platform-dana", DecidedAt: time.Now().UTC(),
	}
}

func revoked(team, app, env string) estate.Admission {
	record := admitted(team, app, env)
	record.State = estate.AdmissionRevoked
	record.Reason = "decommissioned when the service moved to the new ledger"
	record.Reference = "CHG-" + env + "-9902"
	return record
}

func readsBack(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Admit(ctx, admitted("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}
	record, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if !record.Admitted(time.Now().UTC()) {
		t.Fatalf("a freshly onboarded app must read as admitted, got state %q", record.State)
	}
	// The reference is what the chain cites and what a refusal quotes. A store that dropped it
	// would leave every later refusal unable to name a next step.
	if record.Reference == "" || record.DecidedBy == "" {
		t.Fatalf("the store must keep the reference and the decider: %+v", record)
	}
}

// An app the store has never heard of must be reported as MISSING, not returned as a zero value.
//
// A zero Admission has an empty state, which is not "admitted" — so a caller that ignored the
// error would still refuse. But it is also not distinguishable from a revoked one, and "never
// onboarded" and "taken out on purpose" send somebody looking in different places.
func unknownIsRefused(t *testing.T, newStore Factory) {
	store := newStore(t)

	_, err := store.Get(context.Background(), "payments", "payments-ghost", "uat")
	if !errors.Is(err, estate.ErrAdmissionNotFound) {
		t.Fatalf("an app with no record must report ErrAdmissionNotFound, got %v", err)
	}
}

// THE test on the write side. Many onboardings race for one app and environment; exactly one wins.
//
// If two win, the reference an auditor follows is whichever write landed last, and the ticket
// that actually authorised the app to be there is gone with no trace it was ever in force. That
// is the failure this control exists to prevent, arriving through the control itself.
func onlyOneAdmitWins(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	const gatekeepers = 16
	var (
		start    = make(chan struct{})
		wait     sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)
	for gatekeeper := 0; gatekeeper < gatekeepers; gatekeeper++ {
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			<-start // released together, so the window is as narrow as the store allows
			record := admitted("payments", "payments-checkout", "uat")
			record.Reference = fmt.Sprintf("CHG-RACE-%d", n)
			if store.Admit(ctx, record) == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}(gatekeeper)
	}
	close(start)
	wait.Wait()

	if accepted != 1 {
		t.Fatalf("exactly one onboarding may win; %d of %d were accepted — the app is admitted "+
			"under a reference nobody can identify", accepted, gatekeepers)
	}
}

// A second admission over a live one is refused, even with a better reason.
//
// Changing why an app is admitted is a revoke followed by an admit: two acts, two chain
// records. An overwrite would leave one record and no evidence the first decision existed.
func noSilentReadmission(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Admit(ctx, admitted("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}
	second := admitted("payments", "payments-checkout", "uat")
	second.Reference = "CHG-UAT-0002"
	if err := store.Admit(ctx, second); !errors.Is(err, estate.ErrAlreadyAdmitted) {
		t.Fatalf("re-admitting a live admission must fail with ErrAlreadyAdmitted, got %v", err)
	}
	record, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if record.Reference != "CHG-uat-4471" {
		t.Fatalf("the FIRST reference must survive; the record now cites %q", record.Reference)
	}
}

// Revoking something not admitted must not report success.
//
// A silent no-op lets somebody believe they removed an app from an environment it was never in,
// while the environment it IS in keeps deploying it. They stop looking, which is worse than the
// refusal they should have got.
func nothingToRevoke(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	err := store.Revoke(ctx, revoked("payments", "payments-checkout", "uat"))
	if err == nil {
		t.Fatal("revoking an app that was never admitted reported success")
	}
	if !errors.Is(err, estate.ErrAdmissionNotFound) && !errors.Is(err, estate.ErrNothingToRevoke) {
		t.Fatalf("a revoke with nothing to revoke must say so, got %v", err)
	}

	// And the same once a record exists but is already revoked — the second revocation would
	// otherwise overwrite the first one's reason, so the sentence a deployer reads would not
	// belong to the decision that actually removed their app.
	if err := store.Admit(ctx, admitted("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}
	if err := store.Revoke(ctx, revoked("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf("the first revocation must be accepted: %v", err)
	}
	again := revoked("payments", "payments-checkout", "uat")
	again.Reason = "tidying up the inventory"
	if err := store.Revoke(ctx, again); !errors.Is(err, estate.ErrNothingToRevoke) {
		t.Fatalf("a second revocation must be refused with ErrNothingToRevoke, got %v", err)
	}
	record, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if record.Reason == "tidying up the inventory" {
		t.Fatal("the second revocation overwrote the reason of the decision that actually " +
			"removed the app")
	}
}

// A revoked app is not admitted, and the record still explains itself.
//
// The record must be REPLACED, never deleted: a deleted admission reads exactly like an app
// nobody ever onboarded, and "we took this out, here is the ticket" is the single most useful
// thing a refusal can say.
func revokedSaysWhy(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Admit(ctx, admitted("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}
	if err := store.Revoke(ctx, revoked("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(litRevoking, err)
	}

	record, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf("a revoked admission must still be readable, not deleted: %v", err)
	}
	if record.Admitted(time.Now().UTC()) {
		t.Fatal("a revoked app still reads as admitted")
	}
	if record.Reason == "" || record.Reference == "" {
		t.Fatalf("a revocation must carry the reason and the reference a refusal will quote: %+v",
			record)
	}
}

// An app taken out of an environment can be put back, under a new reference.
//
// This is the one overwrite the store allows, and it is not a relaxation of the compare-and-set:
// re-onboarding is a normal act with its own decision, and the record it replaces is already on
// the chain. The store holds what is true now; the chain holds what happened. Without this a
// decommissioned app could never return without somebody editing the store by hand.
func reAdmissionIsAllowed(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Admit(ctx, admitted("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}
	if err := store.Revoke(ctx, revoked("payments", "payments-checkout", "uat")); err != nil {
		t.Fatalf(litRevoking, err)
	}
	back := admitted("payments", "payments-checkout", "uat")
	back.Reference = "CHG-UAT-7788"
	back.Reason = "brought back for the migration cutover"
	if err := store.Admit(ctx, back); err != nil {
		t.Fatalf("re-onboarding a revoked app must be accepted: %v", err)
	}

	record, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if !record.Admitted(time.Now().UTC()) || record.Reference != "CHG-UAT-7788" {
		t.Fatalf("the app must be admitted under the NEW reference, got %+v", record)
	}
}

// A time-boxed admission stops admitting when its time passes, with nothing sweeping it.
//
// Without this an admission granted for a fortnight's experiment outlives the experiment, the
// team, and the reason — and the only way it ever ends is somebody remembering. Evaluated on
// READ so a store with no background loop still cannot hand back a lapsed admission as live.
func expiryIsHonoured(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	lapsing := admitted("payments", "payments-spike", "uat")
	lapsing.ExpiresAt = time.Now().UTC().Add(-time.Minute) // already past
	if err := store.Admit(ctx, lapsing); err != nil {
		t.Fatalf(wrapAdmitting, err)
	}

	record, err := store.Get(ctx, "payments", "payments-spike", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if record.Admitted(time.Now().UTC()) {
		t.Fatal("a lapsed admission still reads as admitted — it would keep admitting forever")
	}
	if record.State != estate.AdmissionExpired {
		t.Fatalf("a lapsed admission must read as expired so a person can tell it from a "+
			"revocation, got %q", record.State)
	}
	// It must also leave the inventory, or the platform keeps expecting the app to be there.
	live, err := store.AdmittedIn(ctx, "uat")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	for _, entry := range live {
		if entry.App == "payments-spike" {
			t.Fatal("a lapsed admission is still listed as admitted")
		}
	}
}

// The environment is an opaque string and the store interprets none of it.
//
// Every case here is a promotion order this module must never assume. A store that normalised,
// ordered, or recognised environment names would admit an app to a place nobody ruled on while
// every record still looked correct — and it would break the first team whose environments are
// not the three a framework author happened to think of.
func envIsOpaque(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	// A promotion order of SIT → DEV | UAT → PROD, plus names no enum would carry.
	environments := []string{"sit", "dev", "uat", "prod", "uat", "UAT", "pre-prod-2",
		"canary-tokyo", "regulator-sandbox", "客戶驗收"}
	for _, env := range environments {
		record := admitted("payments", "payments-checkout", env)
		if err := store.Admit(ctx, record); err != nil && !errors.Is(err, estate.ErrAlreadyAdmitted) {
			t.Fatalf("admitting to env %q: %v", env, err)
		}
	}
	for _, env := range environments {
		record, err := store.Get(ctx, "payments", "payments-checkout", env)
		if err != nil {
			t.Fatalf("env %q must be storable verbatim: %v", env, err)
		}
		if record.Env != env {
			t.Fatalf("env was rewritten from %q to %q", env, record.Env)
		}
	}

	// Case is NOT folded: "uat" and "UAT" are two environments, because only the deployment
	// knows whether they are the same place, and guessing wrong admits an app somewhere nobody
	// ruled on.
	if err := store.Revoke(ctx, revoked("payments", "payments-checkout", "UAT")); err != nil {
		t.Fatalf("revoking from UAT: %v", err)
	}
	lower, err := store.Get(ctx, "payments", "payments-checkout", "uat")
	if err != nil {
		t.Fatalf(wrapReading, err)
	}
	if !lower.Admitted(time.Now().UTC()) {
		t.Fatal("revoking from \"UAT\" also revoked \"uat\" — the store folded case and " +
			"changed a decision nobody made")
	}

	// Admission to one environment is not admission to another. This is the whole control: the
	// app is real, the team is real, the caller is the same caller, and the environment is the
	// only thing that differs.
	if _, err := store.Get(ctx, "payments", "payments-checkout", "prod-dr"); !errors.Is(err, estate.ErrAdmissionNotFound) {
		t.Fatalf("an environment nobody onboarded this app to must have no record, got %v", err)
	}
}

// A decision nobody can be held to must not be storable.
//
// An admission with no reference and no decider answers "may this app be here" perfectly well
// and answers "who said so" not at all, so the refusals it later produces name nobody to ask.
// That is a control that works right up until the day somebody needs to know why.
func attributionIsRequired(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	for _, broken := range []struct {
		what   string
		mangle func(estate.Admission) estate.Admission
	}{
		{"no reference", func(a estate.Admission) estate.Admission { a.Reference = ""; return a }},
		{"no decider", func(a estate.Admission) estate.Admission { a.DecidedBy = ""; return a }},
		{"no reason", func(a estate.Admission) estate.Admission { a.Reason = ""; return a }},
		{"no environment", func(a estate.Admission) estate.Admission { a.Env = ""; return a }},
		{"no app", func(a estate.Admission) estate.Admission { a.App = ""; return a }},
	} {
		record := broken.mangle(admitted("payments", "payments-checkout", "uat"))
		if err := store.Admit(ctx, record); !errors.Is(err, estate.ErrAdmissionUnattributable) {
			t.Errorf("an admission with %s must be refused as unattributable, got %v",
				broken.what, err)
		}
	}
}

func listsOneEnv(t *testing.T, newStore Factory) {
	store := newStore(t)
	ctx := context.Background()

	for _, spec := range []struct{ team, app, env string }{
		{"payments", "payments-checkout", "uat"},
		{"payments", "payments-ledger", "uat"},
		{"treasury", "treasury-reporting", "canary-tokyo"},
	} {
		if err := store.Admit(ctx, admitted(spec.team, spec.app, spec.env)); err != nil {
			t.Fatalf("admitting %s/%s: %v", spec.team, spec.app, err)
		}
	}

	inUAT, err := store.AdmittedIn(ctx, "uat")
	if err != nil {
		t.Fatalf("listing one environment: %v", err)
	}
	if len(inUAT) != 2 {
		t.Fatalf("uat has 2 admitted apps, got %d", len(inUAT))
	}

	// An empty environment means EVERY one — the whole onboarding, which is what the platform
	// reads instead of inferring onboarding from whatever happens to be running.
	everywhere, err := store.AdmittedIn(ctx, "")
	if err != nil {
		t.Fatalf("listing every environment: %v", err)
	}
	if len(everywhere) != 3 {
		t.Fatalf("three apps are admitted overall, got %d", len(everywhere))
	}

	// A revoked one leaves the inventory, or whoever tidies up redeploys a decommissioned app.
	if err := store.Revoke(ctx, revoked("payments", "payments-ledger", "uat")); err != nil {
		t.Fatalf(litRevoking, err)
	}
	inUAT, err = store.AdmittedIn(ctx, "uat")
	if err != nil || len(inUAT) != 1 {
		t.Fatalf("a revoked admission must leave the inventory: %d remain, err=%v", len(inUAT), err)
	}
}
