package config

import estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"

// ownershipDocument is the deployment's additions to field ownership, as lists rather than the
// engine's maps — a list is what an operator writes, and a map of field-to-true in a config
// file invites the question of what `false` would mean.
type ownershipDocument struct {
	// Governed adds fields MantleKeep corrects or escalates on.
	Governed []string `json:"governed"`
	// Watched adds fields worth recording but not ours to change — a second controller owns them.
	Watched []string `json:"watched"`
}

// mergeOntoDefault folds the document's declarations into [estate.DefaultOwnership].
//
// MERGE, not replace, and that asymmetry is the point. Ownership decides whether an unapproved
// change to a field is a violation or a note; a config that could move "digest" from governed to
// watched would turn "the artifact nobody approved is running" into a line in a report, and it
// would do so by ADDING a word to a file rather than by removing a control. So a document may
// add a governed field, add a watched field, or PROMOTE a watched field to governed — and
// nothing it can say demotes or removes one.
//
// A field named in both lists is governed, for the same reason: the weaker claim must not win.
func (o ownershipDocument) mergeOntoDefault() estate.Ownership {
	ownership := estate.DefaultOwnership()
	for _, field := range o.Governed {
		// Promotion is allowed in this direction: a deployment that decides it owns replicas
		// after all is tightening, and tightening is always permitted.
		delete(ownership.Watched, field)
		ownership.Governed[field] = true
	}
	for _, field := range o.Watched {
		if ownership.Governed[field] {
			continue
		}
		ownership.Watched[field] = true
	}
	return ownership
}
