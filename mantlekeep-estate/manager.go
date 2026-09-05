package estate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Manager is the door: every change passes through it before an adapter touches anything.
//
// Layering is deliberate and matches the rest of the estate — a caller resolves WHO is acting,
// the manager decides WHETHER, and the adapter executes. The adapters are given a token they
// did not mint and cannot forge, so a change that reaches Kafka or Kubernetes is a change the
// door allowed.
type Manager struct {
	door  mantlekeep.Submitter
	ports map[string]Port
	// floorOf reads the floor in force RIGHT NOW. A function rather than a value because the
	// floor is reloadable: holding a copy would mean a running server kept governing under the
	// limits it booted with, and the reload would appear to work while changing nothing.
	//
	// Each request calls it ONCE and passes the result down. Reading it per change would let a
	// reload land mid-manifest and produce a decision made half under one revision and half
	// under another — reproducible by nobody.
	floorOf func() Floor
	// discoveries records what the reconciler FOUND, as distinct from what the door decided.
	// Optional: without it drift is still detected and escalated, but an out-of-band change
	// that gets auto-corrected leaves no trace it ever happened.
	discoveries Recorder
	// manifests remembers what each team declared, so a later read — or a reconcile pass with
	// nobody present — can resolve the same footprint without the team posting it again.
	// Optional: without it Apply still governs and applies, but nothing can be read back.
	manifests ManifestStore
	ownership Ownership
	// approvals holds changes awaiting a person. Optional: without it a gated change is still
	// refused, correctly — it simply gives nobody anywhere to stand, which is a gate in name
	// only.
	approvals Approvals
	// placerOf reads the fleet in force right now. A cluster going offline is the most
	// time-critical config change there is — placement keeps choosing it until something says
	// otherwise — so the registry is reloadable for the same reason the floor is.
	placerOf func() *Placer
	// now is injectable so a test can pin intent ids and timestamps rather than assert on a
	// clock it does not control.
	now func() time.Time
}

// NewManager wires the door to the adapters that execute for it.
func NewManager(door mantlekeep.Submitter, floor Floor, ports ...Port) *Manager {
	byAsset := make(map[string]Port, len(ports))
	for _, port := range ports {
		byAsset[port.Asset()] = port
	}
	// placerOf defaults to "no fleet" rather than nil: ResolveWith already refuses a manifest
	// with apps and no placer, with a message about placement. A nil FUNC would panic instead,
	// turning a clear configuration error into a stack trace.
	return &Manager{door: door, ports: byAsset, floorOf: func() Floor { return floor },
		placerOf: func() *Placer { return nil },
		now:      time.Now, ownership: DefaultOwnership()}
}

// RecordDiscoveriesTo makes the reconciler write what it finds. Without a recorder the
// reconciler still corrects and escalates; it simply cannot answer "what changed that nobody
// approved?" afterwards.
func (m *Manager) RecordDiscoveriesTo(recorder Recorder) *Manager {
	m.discoveries = recorder
	return m
}

// GovernFields replaces the default field ownership — which fields are MantleKeep's to correct and
// which belong to the platform.
func (m *Manager) GovernFields(ownership Ownership) *Manager {
	m.ownership = ownership
	return m
}

// RememberManifestsIn makes Apply store the manifest it governed.
//
// The manager stores it rather than the caller, so the remembered declaration is exactly the
// one that went through the door. A caller that stored it separately would have two chances to
// store something else — an older document, or one it edited on the way past — and the estate
// view would then describe a footprint nobody submitted.
func (m *Manager) RememberManifestsIn(store ManifestStore) *Manager {
	m.manifests = store
	return m
}

// FloorFrom makes the manager read a floor that can change while it runs — the config file
// being reloaded rather than the one it booted with.
//
// Without this a manager governs under a snapshot forever, which is correct for a test and
// wrong for a server: an operator who edits a limit and reloads would see the reload succeed
// and the limit stay exactly as it was.
func (m *Manager) FloorFrom(provider func() Floor) *Manager {
	m.floorOf = provider
	return m
}

// AwaitApprovalIn gives gated changes somewhere to wait.
//
// Without a store the door still refuses a change that needs a person, and the refusal still
// says who may sign off — but nothing records the request, so the only way to act on it is to
// re-submit and hope the approver is watching.
func (m *Manager) AwaitApprovalIn(approvals Approvals) *Manager {
	m.approvals = approvals
	return m
}

// PlaceOn supplies the fleet. Without it a manifest containing apps is refused rather than
// placed by default — inventing a cluster would put data somewhere nobody ruled on.
func (m *Manager) PlaceOn(placer *Placer) *Manager {
	m.placerOf = func() *Placer { return placer }
	return m
}

// PlaceOnLive supplies a fleet that can change while the manager runs — a cluster drained,
// added, or found unreachable — without a restart.
func (m *Manager) PlaceOnLive(provider func() *Placer) *Manager {
	m.placerOf = provider
	return m
}

// placedFor reports where a team's apps already run, so placement stays STICKY across passes.
//
// Read from what is OBSERVED, not from what was desired: the desired state is where we intend
// an app to be, and feeding that back would make stickiness circular — it would confirm its own
// last answer rather than the world. An app nobody can see yet is simply unplaced.
func (m *Manager) placedFor(team string) map[string]string {
	placed := map[string]string{}
	for _, port := range m.ports {
		observed, err := port.Observe(context.Background(), team)
		if err != nil {
			// An unreadable asset means we do not know where its apps run. Guessing here would
			// migrate them; leaving the entry absent lets the placer choose afresh only for
			// apps it genuinely cannot locate.
			continue
		}
		for _, item := range observed.Items {
			if item.Asset == "app" && item.Slot.Cluster != "" {
				placed[shortName(item.Name, team)] = item.Slot.Cluster
			}
		}
	}
	return placed
}

// shortName recovers the app name from the deployment name the resolver built.
func shortName(deployment, team string) string {
	return strings.TrimPrefix(deployment, team+"-")
}

// Result is what happened to one change, and why.
type Result struct {
	Change DesiredItem `json:"change"`
	// Refused carries the DOOR's own words when a change did not proceed. Restating the
	// refusal in our vocabulary would give a person two different sentences for one decision,
	// and the one they act on would be ours rather than the policy's.
	Refused string `json:"refused,omitempty"`
	Failed  string `json:"failed,omitempty"`
	// Approval names the record a person can act on, when the refusal was "a person is
	// needed". Empty on a final denial — there is nobody to ask — which is how a caller tells
	// "wait for someone" from "this will never be allowed" without parsing the reason.
	Approval string `json:"approval,omitempty"`
}

// Applied reports whether the change actually reached the asset.
func (r Result) Applied() bool { return r.Refused == "" && r.Failed == "" }

// ApplyOutcome is one pass over a manifest.
type ApplyOutcome struct {
	Applied []Result `json:"applied"`
	Refused []Result `json:"refused"`
	Failed  []Result `json:"failed"`
}

// Apply governs and applies a team's declared footprint.
//
// One intent PER CHANGE, not one for the manifest. The gate is chosen per change — a
// playground topic is instant while a production topic in the same manifest needs a person —
// and a single intent for the whole document would collapse that into the strictest gate,
// which is the over-gating that makes a golden path slower than the bypass.
//
// A refusal does NOT abort the rest. Blocking every dev resource because one prod resource
// awaits approval is the same over-gating by another route; the refused change simply does not
// happen, and says so.
func (m *Manager) Apply(ctx context.Context, actor mantlekeep.Subject, manifest Manifest) (ApplyOutcome, error) {
	floor := m.floorOf()
	desired, err := ResolveWith(manifest, floor, m.placerOf(), m.placedFor(manifest.Team))
	if err != nil {
		return ApplyOutcome{}, err
	}
	// Remembered once it RESOLVES, before anything is applied. A manifest whose every change is
	// refused is still the team's declaration, and the estate view is built to show exactly
	// that — a gated resource that is approved-but-absent until a person signs off. Storing it
	// only on success would erase the pending half of the footprint.
	if m.manifests != nil {
		if err := m.manifests.Remember(ctx, manifest); err != nil {
			return ApplyOutcome{}, fmt.Errorf("%w: remember %s: %v", ErrStoreFailure, manifest.Team, err)
		}
	}

	var outcome ApplyOutcome
	for _, change := range desired.Changes {
		result := m.applyOne(ctx, acting{subject: actor}, manifest.Team, change, floor.Revision)
		switch {
		case result.Refused != "":
			outcome.Refused = append(outcome.Refused, result)
		case result.Failed != "":
			outcome.Failed = append(outcome.Failed, result)
		default:
			outcome.Applied = append(outcome.Applied, result)
		}
	}
	return outcome, nil
}

// acting is WHO the door is being asked about: the subject making THIS submission and, when the
// submission is an APPROVAL, the person who originally asked for the change.
//
// They are grouped rather than passed as two bare strings because they are one fact — "who is
// acting, and on whose request" — and because two interchangeable identity arguments side by side
// is precisely how the requester came to be the actor in the first place.
type acting struct {
	// subject is who the door rules on and who the chain records.
	subject mantlekeep.Subject
	// requester is EMPTY on a submission: the subject is the person asking, and claiming
	// otherwise makes separation of duties compare somebody with themselves. It is filled only
	// on the approval path, from the stored approval record.
	requester string
}

// applyOne submits one change to the door and, only on allow, hands it to the adapter.
func (m *Manager) applyOne(ctx context.Context, who acting, team string,
	change DesiredItem, floorRevision string) Result {

	port, ok := m.ports[change.Asset]
	if !ok {
		// No adapter means nothing can execute this. Reporting it beats submitting an intent
		// that would be allowed and then silently do nothing — an approval for work that never
		// happened is worse than a refusal, because the record says it was fine.
		return Result{Change: change,
			Failed: fmt.Sprintf("no adapter is registered for asset %q", change.Asset)}
	}

	token, err := m.door.Submit(ctx, m.intentFor(who, team, change, floorRevision))
	if err != nil {
		// A change awaiting a person is not the same as one that is forbidden. Record it, so
		// somebody can act on it, and hand the caller a reference rather than a dead end.
		var refused *mantlekeep.Refused
		if m.approvals != nil && errors.As(err, &refused) && refused.Pending() {
			if id, openErr := m.openApproval(ctx, who.subject, team, change, floorRevision,
				refused); openErr == nil {
				return Result{Change: change, Refused: err.Error(), Approval: id}
			}
			// Falling through on a store failure is deliberate: the change is still refused, and
			// reporting it as applied because the queue was unavailable would be far worse.
		}
		return Result{Change: change, Refused: err.Error()}
	}
	// The WHOLE token. The adapter authorises with token.Value and records token.IntentID; a
	// bare Value left it with only the capability to record, which is how a live token ended up
	// in a Kubernetes annotation.
	if err := port.Apply(ctx, token, change); err != nil {
		return Result{Change: change, Failed: err.Error()}
	}
	return Result{Change: change}
}

// requesterFor names who ASKED, for the door's separation-of-duties rule — and only on the
// approval path, where a second person is genuinely being asserted.
//
// A SUBMISSION has no requester. The acting subject IS the person asking, so naming them here
// made subject == requester on every gated change: the door's SoD rule — a rule about APPROVAL —
// then fired on the request itself and refused it flatly, with nobody to ask and no approval to
// point at. A submission must never claim to be its own approval.
//
// Empty on an ungated change even when a requester is supplied, because there is no approval for
// a requester to be separate from.
func requesterFor(requester string, change DesiredItem) string {
	if change.Gate == GateNone {
		return ""
	}
	return requester
}

// intentFor describes the change to the door in the door's own vocabulary.
//
// The tier and gate travel as params so policy can rule on CONSEQUENCE rather than having to
// know what a Kafka topic is. That is what lets one policy govern every asset: the door reads
// blast radius, not technology.
func (m *Manager) intentFor(who acting, team string, change DesiredItem,
	floorRevision string) mantlekeep.Intent {
	return mantlekeep.Intent{
		ID:      fmt.Sprintf("ESTATE-%s-%s-%d", team, change.Name, m.now().UnixNano()),
		Subject: who.subject,
		Action:  "estate.apply",
		// Scope is the team's namespace, so a policy can say "this team, this project" without
		// enumerating resources it will never have heard of.
		Resource: "team/" + team,
		Spec: mantlekeep.IntentSpec{
			Goal: fmt.Sprintf("provision %s %s %q for %s", change.Asset, change.Kind,
				change.Name, team),
		},
		Params: map[string]any{
			// The door reads "requester" to enforce separation of duties (see the core's
			// SDK.Submit), and it is the ORIGINAL requester — never the caller. On a
			// submission it is empty: the subject is the person asking, and naming them makes
			// the door compare somebody with themselves and refuse the request before anybody
			// could approve it.
			//
			// It is also what SATISFIES the door's approval gate. A second party is present
			// exactly when this names somebody other than the acting subject, so an approval
			// proceeds where the original request was told to wait. Without it on the approval
			// call the rule cannot fire at all: the policy compares the approver against a
			// value nobody supplied, and every self-approval passes while the code looks like
			// it is checking.
			"requester": requesterFor(who.requester, change),
			"asset":     change.Asset,
			"kind":      change.Kind,
			"name":      change.Name,
			"tier":      string(change.Tier),
			"gate":      string(change.Gate),
			"cluster":   change.Cluster,
			"scope":     team,
			// WHICH floor decided. The limits on this change came from a file that can be
			// edited while the server runs, so without the revision a record from last year
			// cannot be read against the rules that were actually in force — an approval
			// today's floor would refuse is indistinguishable from a mistake.
			"floorRevision": floorRevision,
		},
		SubmittedAt: m.now().UTC(),
		TTL:         5 * time.Minute,
	}
}

// Reconcile observes reality, compares it with the approved footprint, and closes the gap.
//
// Every correction goes through the door too. A reconciler that corrects without governing is
// making ungoverned changes on a timer, which is worse than a human doing it once: it is
// unattributed AND repeated. Drift that needed a human to exist still needs one to be
// corrected, so it is escalated rather than re-applied.
func (m *Manager) Reconcile(ctx context.Context, actor mantlekeep.Subject,
	manifest Manifest) (ApplyOutcome, []Drift, error) {

	floor := m.floorOf()
	desired, err := ResolveWith(manifest, floor, m.placerOf(), m.placedFor(manifest.Team))
	if err != nil {
		return ApplyOutcome{}, nil, err
	}

	// Same observation the read path uses. One definition of "reality", or the reconciler
	// eventually corrects against a picture the estate view never showed.
	observed, err := observeAll(ctx, m.ports, manifest.Team)
	if err != nil {
		return ApplyOutcome{}, nil, err
	}

	var outcome ApplyOutcome
	var escalated []Drift
	for _, drift := range DiffOwned(desired, observed, m.ownership) {
		// Record BEFORE acting. A correction applied first and recorded second loses the fact
		// that anything was ever wrong if the record fails — and that is the one fact the
		// reconciler exists to produce.
		m.recordDiscovery(ctx, drift)
		if !drift.Correctable() {
			escalated = append(escalated, drift)
			continue
		}
		result := m.applyOne(ctx, acting{subject: actor}, manifest.Team, *drift.Desired,
			floor.Revision)
		switch {
		case result.Refused != "":
			outcome.Refused = append(outcome.Refused, result)
		case result.Failed != "":
			outcome.Failed = append(outcome.Failed, result)
		default:
			outcome.Applied = append(outcome.Applied, result)
		}
	}
	return outcome, escalated, nil
}

// recordDiscovery notes drift on the chain. A failure to record is not allowed to stop the
// pass: losing one record is bad, but a reconciler that halts on it stops correcting anything
// at all, and then reality drifts further while nobody is watching.
func (m *Manager) recordDiscovery(ctx context.Context, drift Drift) {
	if m.discoveries == nil {
		return
	}
	slot := Slot{}
	switch {
	case drift.Desired != nil:
		slot = drift.Desired.Slot
	case drift.Observed != nil:
		slot = drift.Observed.Slot
	}
	_ = m.discoveries.Record(ctx, Discovery{
		Slot: slot, Kind: drift.Kind, Detail: drift.Detail, Observed: m.now().UTC(),
	})
}

// openApproval records a change the door said needs a person.
//
// The RESOLVED change is stored, not the manifest: re-resolving at approval time would apply
// whatever the manifest and floor say then, which is not what anybody signed off. The floor
// revision is stored for the same reason and checked again on approval.
func (m *Manager) openApproval(ctx context.Context, actor mantlekeep.Subject, team string,
	change DesiredItem, floorRevision string, refused *mantlekeep.Refused) (string, error) {

	now := m.now().UTC()
	roles := make([]string, 0, len(refused.RequiredApprovers))
	for _, role := range refused.RequiredApprovers {
		roles = append(roles, string(role))
	}
	approval := Approval{
		ID:            approvalID(team, change.Name, now),
		Team:          team,
		Change:        change,
		Requester:     actor.ID,
		RequiredRoles: roles,
		FloorRevision: floorRevision,
		Reason:        refused.Reason,
		State:         ApprovalPending,
		CreatedAt:     now,
		// A request nobody acts on must not sit forever: a queue of stale approvals is
		// indistinguishable from a queue of live ones, and people stop reading both.
		ExpiresAt: now.Add(m.approvalWindow()),
	}
	if err := m.approvals.Open(ctx, approval); err != nil {
		return "", err
	}
	return approval.ID, nil
}

// approvalWindow is how long a pending change waits for a person.
//
// Seven days rather than a tunable, for now: a window short enough that a forgotten request
// expires rather than accumulating, and long enough to survive a holiday. It becomes floor
// config the first time a deployment disagrees.
func (m *Manager) approvalWindow() time.Duration { return 7 * 24 * time.Hour }

// Approve applies a change a person has signed off.
//
// Every rule here is enforced in CODE rather than by the caller, because each of them is a way
// the two-party guarantee is lost quietly:
//
//   - the requester may not approve their own change — a two-party rule satisfied by one party
//     is not a rule, and no configuration reaches this
//   - the ROLE is not checked here, deliberately. caller.go states the rule: "Roles are the
//     door's to resolve from the directory; a caller that could assert its own roles could
//     assert its way past any gate." So the change is submitted AS the approver and the door
//     refuses if they lack the role — RequiredRoles on the record is there to tell a person who
//     to ask, never to authorise them
//   - the floor must not have moved, or what is applied is not what was approved
//   - and the record is decided BEFORE the change is applied, so a crash between them leaves a
//     signed approval and an unapplied change rather than an applied change nobody signed
func (m *Manager) Approve(ctx context.Context, approver mantlekeep.Subject,
	id string) (Result, error) {

	if m.approvals == nil {
		return Result{}, fmt.Errorf("estate: no approval store is configured")
	}
	approval, err := m.approvals.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if !approval.Pending(m.now().UTC()) {
		return Result{}, ErrApprovalNotPending
	}
	if approval.Requester == approver.ID {
		return Result{}, ErrSelfApproval
	}
	if current := m.floorOf().Revision; current != approval.FloorRevision {
		return Result{}, fmt.Errorf("%w (approved under %s, now %s)",
			ErrFloorMoved, approval.FloorRevision, current)
	}

	// Recorded first. If applying then fails, the record says approved-and-not-applied, which a
	// reconcile pass converges; the reverse order would apply a change whose approval was lost.
	approval.State = ApprovalApproved
	approval.ApprovedBy = approver.ID
	approval.DecidedAt = m.now().UTC()
	if err := m.approvals.Decide(ctx, approval); err != nil {
		return Result{}, err
	}

	// Submitted as the APPROVER, so the door sees a second party and the chain names who
	// unblocked it. Submitting as the requester would put one name on a two-party decision.
	// Submitted as the APPROVER, naming the REQUESTER. Those two names are what the door's
	// separation-of-duties rule compares, and carrying both is what turns this submission into
	// an approval rather than a second identical request.
	return m.applyOne(ctx, acting{subject: approver, requester: approval.Requester},
		approval.Team, approval.Change, approval.FloorRevision), nil
}

// Decline records that a person refused, with their reason.
//
// A reason is required. "Declined" with no words sends the requester back to whoever they can
// find, and the next thing they try is the path that does not ask.
func (m *Manager) Decline(ctx context.Context, approver mantlekeep.Subject, id, reason string) error {
	if m.approvals == nil {
		return fmt.Errorf("estate: no approval store is configured")
	}
	if reason == "" {
		return fmt.Errorf("estate: a decline needs a reason — without one the requester has " +
			"nothing to act on except finding another route")
	}
	approval, err := m.approvals.Get(ctx, id)
	if err != nil {
		return err
	}
	if !approval.Pending(m.now().UTC()) {
		return ErrApprovalNotPending
	}
	approval.State = ApprovalDeclined
	approval.DeclinedBy = approver.ID
	approval.DeclinedReason = reason
	approval.DecidedAt = m.now().UTC()
	return m.approvals.Decide(ctx, approval)
}

// PendingApprovals lists what is waiting, for one team or all of them.
func (m *Manager) PendingApprovals(ctx context.Context, team string) ([]Approval, error) {
	if m.approvals == nil {
		return nil, nil
	}
	return m.approvals.Pending(ctx, team)
}
