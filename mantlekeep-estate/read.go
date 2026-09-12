package estate

import (
	"context"
	"errors"
	"fmt"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// ErrReadRefused reports that the door would not let this subject read this team.
//
// Deliberately indistinguishable, to a caller, from a team that does not exist. A refusal that
// says "you may not see this" confirms there IS something to see, and for a read whose whole risk
// is disclosure, the existence of another team's estate is part of what is being withheld. The
// API layer renders both as 404 for the same reason.
var ErrReadRefused = errors.New("estate: this subject may not read this team")

// ErrNoFootprintReader reports that nothing was wired to answer reads.
//
// An error rather than an empty footprint: a read side that quietly answers nothing looks like a
// team with no estate, and "you have declared nothing" is a materially different statement from
// "this server cannot tell you".
var ErrNoFootprintReader = errors.New(
	"estate: no footprint reader is configured — see Manager.ReadFootprintsFrom")

// FootprintReader resolves a team's estate once the door has allowed the read.
//
// An interface so the manager can govern a read without absorbing the read side's job. [Service]
// satisfies it.
type FootprintReader interface {
	Footprint(ctx context.Context, team string) (Footprint, error)
}

// ReadFootprintsFrom gives the manager the read side to delegate to once a read is allowed.
//
// Wire this wherever a Service and a Manager are built together. Without it [Manager.Footprint]
// refuses every read: failing closed is the only safe default for the call that exists to stop a
// team reading another team's estate.
func (m *Manager) ReadFootprintsFrom(reader FootprintReader) *Manager {
	m.footprints = reader
	return m
}

// Footprint returns a team's estate, if the door allows this subject to read it.
//
// # Why a read is governed at all
//
// Reads used to go straight to the service, on the stated reasoning that "nothing happens as a
// result of a read and there is no decision to record". That holds for mutation and fails for
// disclosure. Nothing HAPPENS; something is REVEALED — a team's manifest, and its drift report
// naming which approved resources do not yet exist, which is reconnaissance rather than mere
// disclosure. Any authenticated caller could name any team in the URL and receive it.
//
// The team is not checked here against some attribute of the subject, because [mantlekeep.Subject]
// deliberately carries no team: roles and groups come from the directory, and a caller that could
// assert its own scope could assert its way past any gate. Who may read which team is a POLICY
// question, so it is asked of the door — the same door, on the same chain, as every write.
//
// # What a deployment must do
//
// estate.read is its own action, so a policy can grant reading without granting writing. Like
// estate.apply it is refused until a deployment grants it: the door answers "no role permits
// action estate.read" by default. Granting it is the same kind of act as granting estate.apply,
// and reads stop working until it happens — loudly, which is the intended failure direction for a
// control whose absence is invisible.
func (m *Manager) Footprint(ctx context.Context, actor mantlekeep.Subject,
	team string) (Footprint, error) {

	if m.footprints == nil {
		return Footprint{}, ErrNoFootprintReader
	}

	// Governed BEFORE the read, not after. A read that resolves first and checks second has
	// already loaded the thing it was deciding whether to disclose, and every later change to
	// this function is one refactor away from returning it.
	if _, err := m.door.Submit(ctx, m.intentForRead(actor, team)); err != nil {
		return Footprint{}, fmt.Errorf("%w: %w", ErrReadRefused, err)
	}
	return m.footprints.Footprint(ctx, team)
}

// intentForRead builds the intent a read submits.
//
// Shaped like [Manager.intentFor] on purpose — same id scheme, same team-scoped resource — so one
// policy can reason about reads and writes of the same team in the same terms. It carries no
// execution params because a read provisions nothing: there is no asset, no tier and no gate,
// and inventing them would put values on the chain that no adapter ever acted on.
func (m *Manager) intentForRead(actor mantlekeep.Subject, team string) mantlekeep.Intent {
	return mantlekeep.Intent{
		ID:      fmt.Sprintf("ESTATE-READ-%s-%d", team, m.now().UnixNano()),
		Subject: actor,
		// Its own action, so reading can be granted separately from writing. Folding reads into
		// estate.apply would mean any role allowed to look is allowed to change.
		Action: "estate.read",
		// The team being READ, which is the scope the door must rule on. The subject's own team
		// is not what is in question: the whole failure was a caller naming somebody else's.
		Resource: "team/" + team,
		Spec: mantlekeep.IntentSpec{
			Goal: fmt.Sprintf("read the estate of %s", team),
		},
		Params: map[string]any{
			"scope": team,
		},
		SubmittedAt: m.now().UTC(),
		TTL:         readIntentTTL,
	}
}

// readIntentTTL is short because a read is answered immediately. The token is never handed to an
// adapter — nothing executes — so the window only has to cover this call.
const readIntentTTL = time.Minute
