package app

import (
	"context"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/grants"
	"github.com/mantlekeep/mantlekeep/mantlekeep-control/internal/policy"
)

// This file is the READ side of policy: how a surface asks what the law currently is, without
// asking the door to decide anything and without becoming a second copy of the law itself.

// PolicyInForce is a [grants.Loader] over the documents the door's engine is EVALUATING
// AGAINST — not the ones on disk.
//
// The difference is the entire reason it exists. [grants.EnvSource] re-reads the configured
// files, which is what a policy CHANGE needs; a screen that shows people who may do what needs
// the opposite, because a file the engine has not ACCEPTED is a file, not a law. The two
// diverge precisely when it matters most — right after someone saves a policy the engine
// refused — and a screen fed by EnvSource would show that file and be believed.
//
// Because it is the same port, a policy change and the screen that displays it agree on the
// revision: [grants.Govern] records the revision it read as "before", and this is the revision
// the reader was looking at when they asked for the change.
type PolicyInForce struct{}

var _ grants.Loader = PolicyInForce{}

// Load returns the engine's own documents and the revision that identifies them.
//
// The revision is derived from the shape returned HERE, by the same [grants.RevisionOfDocuments]
// every other producer uses, so it always identifies exactly what was handed back.
//
// It therefore does NOT equal the revision [grants.EnvSource] reports for the same policy, and
// [policy.RevisionInForce] — the DOCUMENT revision the engine accepted — is a third value again.
// The reason is that the view below is not a document: it carries the engine's built-in
// L0-SuperAdmin wildcard, which is code, present in no policy file. Whether that is right is an
// open question, because it means a revision shown on a screen cannot be matched against a
// revision derived from documents. What is NOT in question is that this revision identifies what
// this loader returned, which is what a caller comparing two replicas needs.
func (PolicyInForce) Load(context.Context) (*grants.Grants, *grants.Floors, grants.Revision, error) {
	held, floors := policy.InForce()
	return held, floors, grants.RevisionOfDocuments(held, floors), nil
}

// EvaluationOrder publishes the order the default engine asks its questions in.
//
// Exposed because a refusal is only actionable when a person can see WHICH stage produced it and
// whether that stage is a document they may argue with or an engine floor they may not. The list
// is published by the engine and held to the engine's real behaviour by a test, so a surface that
// renders it is quoting the code rather than describing it.
//
// It describes the CORE's default engine. A deployment that injects its own evaluator is not
// described by this, and a surface must say so rather than present it as the order in force.
func EvaluationOrder() []mantlekeep.EvaluationStep { return policy.EvaluationOrder() }
