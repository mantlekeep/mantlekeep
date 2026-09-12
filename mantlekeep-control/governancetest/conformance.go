// Package governancetest is the conformance suite a DEPLOYMENT must pass. It answers one
// question the port-level suites cannot: is this deployment actually configured to govern?
//
// chaintest and approvalstest prove that an IMPLEMENTATION honours its contract. Both can pass
// on a deployment that governs nothing, because the failure is not in the code — it is in the
// policy documents, which ship EMPTY on purpose (grants/grants.json is `{"role_actions":{},
// "approval_actions":[]}`) so that the core binary carries no policy of its own. A deployment
// that never writes them gets an engine in perfect working order with nothing to enforce.
//
// Two of those states are loud and two are silent, and the silent ones are the reason this
// package exists:
//
//   - No grants ⇒ every action denied. Fails CLOSED. Somebody notices within minutes.
//   - No approval_actions ⇒ the "an AI may never approve" seal has an empty set to match
//     against. Real code, no ammunition.
//   - A gated action with no require_approval_when rule ⇒ the gate param is computed, travels
//     on the intent, and is written to the hash chain, and NOTHING reads it. Fails OPEN, and
//     the audit record says the change was gated. This is the dangerous one.
//   - A floor rule for an action no role is granted ⇒ unreachable code. The role check runs
//     BEFORE the floor (internal/policy/precedence.go), so the rule never gets asked.
//
// Every finding below names the document to edit, the key inside it, and what goes wrong if it
// is left alone.
//
// # Run it IN THE DOOR'S OWN PROCESS, and set the policy environment before the first submit
//
// The engine reads the policy documents ONCE per process and holds that snapshot for the life
// of the process (internal/policy/grants_live.go seeds it behind a sync.Once; ReloadPolicy can
// replace it, re-reading the environment cannot). So:
//
//   - Two policy variants CANNOT be A/B tested in one process. To exercise a good policy and a
//     bad one, use two test packages — `go test` gives each its own binary — or two runs with
//     different environments. A t.Setenv in a second sub-test changes nothing the door reads.
//   - The suite reads the documents through grants.Load(), which DOES re-read the environment.
//     That is a way to be told about a policy the door is not using. documentsInForce therefore
//     compares the revision it just loaded with the revision the engine is deciding against and
//     refuses to report on a mismatch, rather than naming keys in a document nobody is enforcing.
//
// This also means the Door must be in-process. An HTTP client to a remote door would be judged
// against THIS process's environment, which is a different deployment wearing the same name.
package governancetest

import (
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/doorkit"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
)

// The documents a finding tells an operator to go and edit. Named once, because a message that
// sends somebody to the wrong file costs more than no message at all.
const (
	grantsDocument = "the grants document (MANTLEKEEP_POLICY_GRANTS, the platform doc in " +
		"MANTLEKEEP_PLATFORM_POLICY, or a product doc in MANTLEKEEP_POLICY_DIR)"
	floorsDocument = "the floors document (MANTLEKEEP_POLICY_FLOORS, the platform doc in " +
		"MANTLEKEEP_PLATFORM_POLICY, or a product doc in MANTLEKEEP_POLICY_DIR)"
)

// Action is ONE product action as the deployment's intents really carry it.
//
// Params is a REPRESENTATIVE intent's params, not a schema: the gate rules are matched against
// param values, so a rule that reads whenParam "gate" whenValue "platform" is only proved to
// fire by an intent that actually carries gate=platform. A params map with the right keys and
// the wrong values is exactly the defect this suite looks for in the document, and supplying
// one here would hide it.
type Action struct {
	// Name is the action name the door is submitted with, e.g. "topic.create".
	Name string
	// Params are a representative intent's params, e.g. {"gate":"platform","tier":"prod"}.
	Params map[string]any
	// Gated is the deployment ASSERTING that this action must not proceed on one person's
	// say-so. It is a claim to be checked, not a fact to be trusted — the whole point is to
	// find out whether the documents agree.
	Gated bool
}

// Deployment is what a deployment hands the suite: its door, the people it knows about, and the
// actions its products issue.
type Deployment struct {
	// Door is the assembled door, IN THIS PROCESS. See the package comment.
	Door mantlekeep.Submitter
	// Subjects are ids the door's identity resolver can resolve — at least two DIFFERENT
	// people, so separation of duties has a second person to be separate from, and ideally
	// one IsAI so the AI seal can be fired rather than assumed.
	//
	// Only ID and IsAI are read. Roles are NOT: the door resolves them server-side from its
	// directory and never trusts a caller's claim (internal/sdk/sdk.go), so a Roles slice set
	// here would describe a subject the door does not have. IsAI is read only to decide which
	// id to point at the AI seal; whether that id really is an AI is the directory's answer,
	// and a disagreement is itself reported.
	Subjects []mantlekeep.Subject
	// Actions are the product's actions and the params its intents carry.
	Actions []Action
}

// Run executes the whole suite against one deployment.
func Run(t *testing.T, deployment Deployment) {
	t.Helper()
	refuseUnusableDeployment(t, deployment)
	held, floors := documentsInForce(t)

	// The document checks: cheap, exact about which key is wrong.
	t.Run("some role is granted some action", func(t *testing.T) {
		someRoleIsGrantedSomeAction(t, held)
	})
	t.Run("the AI-cannot-approve seal has something to fire on", func(t *testing.T) {
		theAISealHasAmmunition(t, held)
	})
	t.Run("every gated action has a rule that gates it", func(t *testing.T) {
		everyGatedActionHasARuleThatFires(t, deployment, floors)
	})
	t.Run("no floor rule is unreachable", func(t *testing.T) {
		noFloorRuleIsUnreachable(t, held, floors)
	})

	// The behavioural checks: slower, but they are the engine's own answer, so they also cover
	// grants that arrive from a registered provider or a config layer rather than a document.
	t.Run("some supplied subject can reach some declared action", func(t *testing.T) {
		someSubjectCanReachSomeAction(t, deployment)
	})
	t.Run("a gated action waits for a second person", func(t *testing.T) {
		aGatedActionWaitsForASecondPerson(t, deployment)
	})
	t.Run("nobody approves their own change", func(t *testing.T) {
		nobodyApprovesTheirOwnChange(t, deployment)
	})
	t.Run("an AI is never the approver", func(t *testing.T) {
		anAIIsNeverTheApprover(t, deployment, held)
	})
}

// refuseUnusableDeployment fails before any check runs when the suite was given inputs it cannot
// draw a conclusion from.
//
// It fails rather than skips. A suite that quietly tests nothing because it was handed one
// subject reports a green deployment, and green is the answer everybody acts on.
func refuseUnusableDeployment(t *testing.T, deployment Deployment) {
	if deployment.Door == nil {
		t.Fatal("governancetest: no Door — pass the assembled door from this process, because " +
			"every behavioural check is that door's own answer and nothing else can stand in for it")
	}
	if len(deployment.Actions) == 0 {
		t.Fatal("governancetest: no Actions — pass the product's actions and the params its " +
			"intents carry, or the suite can only confirm that an unused engine is idle")
	}
	distinct := map[string]bool{}
	humans := 0
	for _, subject := range deployment.Subjects {
		if subject.ID == "" {
			t.Fatal("governancetest: a Subject has no ID — the door resolves subjects by id, so " +
				"an empty one cannot be resolved and its refusal would prove nothing")
		}
		distinct[subject.ID] = true
		if !subject.IsAI {
			humans++
		}
	}
	if len(distinct) < 2 {
		t.Fatalf("governancetest: %d distinct Subject id(s) — separation of duties needs a "+
			"SECOND person to be separate from, and with one id every approval looks like a "+
			"self-approval whether the floor works or not", len(distinct))
	}
	if humans == 0 {
		t.Fatal("governancetest: every Subject is marked IsAI — an AI can never satisfy an " +
			"approval gate, so no gate check could distinguish a working floor from a broken one")
	}
}

// documentsInForce loads the grant and floor documents and refuses to go on unless they are the
// documents the engine is actually deciding against.
//
// The comparison is the point. grants.Load() re-reads the environment on every call; the engine
// does not (see the package comment). Without this, a suite whose environment was set after the
// door's first submit would read the new documents, name keys in them, and pass or fail on a
// policy nobody is enforcing — reporting confidently on the wrong deployment, which is worse
// than reporting nothing.
func documentsInForce(t *testing.T) (*grants.Grants, *grants.Floors) {
	held, err := grants.Load()
	if err != nil {
		t.Fatalf("loading %s: %v — the door will not start on a document that does not parse, "+
			"so fix this before reading anything else here", grantsDocument, err)
	}
	floors, err := grants.LoadFloors()
	if err != nil {
		t.Fatalf("loading %s: %v — the door will not start on a document that does not parse, "+
			"so fix this before reading anything else here", floorsDocument, err)
	}
	// Asked AFTER the load, so that a process which has not yet seeded its snapshot seeds it
	// from the very documents just read, and the two agree by construction.
	if loaded, inForce := grants.RevisionOfDocuments(held, floors), doorkit.PolicyRevisionInForce(); loaded != inForce {
		t.Fatalf("the policy documents in the environment (revision %s) are NOT the documents "+
			"this door is deciding against (revision %s): the engine read its policy once, at "+
			"the first governed call, and holds it for the life of the process. Set the "+
			"MANTLEKEEP_POLICY_* environment in TestMain BEFORE anything submits, and use a "+
			"separate test package for each policy variant", loaded, inForce)
	}
	return held, floors
}
