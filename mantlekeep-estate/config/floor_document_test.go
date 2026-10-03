package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// These tests pin what a floor DOCUMENT resolves TO. The rest of this package's tests pin what a
// document is REFUSED for, which is the other half and the easier half to remember to write.
//
// The gap they close was found in a live deployment: its estate-floor.json declares no "gates"
// block and no "envTiers" block, so both come entirely from the built-in defaults. That is the
// correct answer and it works — but nobody had asserted anybody MEANT it. Change
// [estate.DefaultGates] or [floorDocument.toFloor] and every gate that deployment applies moves,
// silently, with every test in this package still green.
//
// The three default environments are named rather than written inline because the environment
// namespace and the tier namespace share two of their words: a bare "prod" in an assertion does
// not say which of the two it is, and they are not interchangeable ("sit" is an environment whose
// tier is "shared").
const (
	envDev  = "dev"
	envSit  = "sit"
	envProd = "prod"
)

// envNobodyRuled is an environment the built-in table says nothing about. Used to pin that an
// unruled environment reads as UNKNOWN rather than as harmless.
const envNobodyRuled = "uat"

// mustReplace edits the known-good document, failing loudly when the text it was told to change
// is not there or is there more than once.
//
// [strings.Replace] returns the document UNCHANGED when it matches nothing, so in a refusal test a
// stale anchor hands Parse a perfectly valid document and the case fails with "this was accepted" —
// a message that blames the engine when the FIXTURE is what moved. An anchor that matches twice is
// just as bad: it silently mutates whichever line came first.
func mustReplace(t *testing.T, find, replace string) string {
	t.Helper()
	switch strings.Count(validDocument, find) {
	case 1:
		return strings.Replace(validDocument, find, replace, 1)
	case 0:
		t.Fatalf("fixture no longer contains %q, so this case is not testing what it claims to",
			find)
	default:
		t.Fatalf("fixture contains %q more than once — this edit would land on an arbitrary one",
			find)
	}
	return ""
}

// withFloorSection splices an optional floor section into the known-good document, so a case
// differs from a PASSING document by exactly the section under test. Mirrors withGates and
// withApps, generalised over the key because these tests need "envTiers" and "revision" too.
func withFloorSection(t *testing.T, key, body string) string {
	t.Helper()
	const anchor = `    "fleet": {`
	return mustReplace(t, anchor, `    "`+key+`": `+body+`,
`+anchor)
}

// A document that names no gates resolves to the built-in ladder, tier by tier.
//
// WHY: the live deployment declares no "gates" block, so these three values ARE the gates it
// applies — dev instant, shared owning-team, prod platform. Without this test that is an accident
// of whatever DefaultGates happens to return rather than a stated expectation, and re-gating (or
// un-gating) a running footprint would take a one-line change nothing here would notice.
//
// Asserted against Floor.Gates DIRECTLY, not through GateFor: [estate.Floor.GateFor] falls back to
// DefaultGates for a tier it has no entry for, so it answers correctly even for a floor whose
// table was never populated at all. The stored table is what the API serialises out and what
// validateGates reads, so the stored table is what has to be right.
func TestAFloorDocumentNamingNoGatesResolvesToTheBuiltInLadder(t *testing.T) {
	config, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	for _, expected := range []struct {
		tier estate.Tier
		gate estate.Gate
	}{
		{estate.TierDev, estate.GateNone},
		{estate.TierShared, estate.GateOwningTeam},
		{estate.TierProd, estate.GatePlatform},
	} {
		got, floored := config.Floor.Gates[expected.tier]
		if !floored {
			t.Errorf("tier %q has no gate in the resolved floor — a document that names no "+
				"gates must inherit the WHOLE built-in ladder, not an empty table", expected.tier)
			continue
		}
		if got != expected.gate {
			t.Errorf("tier %q resolved to gate %q, want %q — that is the gate a deployment "+
				"declaring no \"gates\" block actually applies", expected.tier, got, expected.gate)
		}
	}
	if len(config.Floor.Gates) != len(estate.DefaultGates()) {
		t.Errorf("the resolved floor carries %d gates, want the %d in DefaultGates() — a tier "+
			"gained or lost here is a tier whose cost of change nobody chose",
			len(config.Floor.Gates), len(estate.DefaultGates()))
	}
}

// A document that names no environment tiers resolves to the built-in minimums.
//
// WHY: EnvTiers is the control that stops a manifest declaring tier "dev" against a production
// cluster. The live deployment declares no "envTiers" block, so that control is entirely the
// built-in table — and an empty table fails no validator, because validateEnvTiers only walks the
// entries that ARE there. An empty EnvTiers un-floors every environment at once and nothing else
// in this package would say so.
//
// Asserted against Floor.EnvTiers DIRECTLY for the same reason the gates above are:
// [estate.Floor.MinTierFor] consults DefaultEnvTiers for an environment the table is missing, so
// it answers correctly even for a floor whose table was never populated. Through the accessor an
// empty table is INVISIBLE; only the stored one shows it.
func TestAFloorDocumentNamingNoEnvironmentTiersResolvesToTheBuiltInMinimums(t *testing.T) {
	config, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	for _, expected := range []struct {
		environment string
		minimum     estate.Tier
	}{
		{envDev, estate.TierDev},
		{envSit, estate.TierShared},
		{envProd, estate.TierProd},
	} {
		got, ruled := config.Floor.EnvTiers[expected.environment]
		if !ruled {
			t.Errorf("environment %q is unruled in the resolved floor — a manifest could then "+
				"declare its own tier there and pick its own gate", expected.environment)
			continue
		}
		if got != expected.minimum {
			t.Errorf("environment %q floors at tier %q, want %q — that is the least consequence "+
				"a deployment declaring no \"envTiers\" block actually enforces there",
				expected.environment, got, expected.minimum)
		}
	}
	if len(config.Floor.EnvTiers) != len(estate.DefaultEnvTiers()) {
		t.Errorf("the resolved floor rules %d environments, want the %d in DefaultEnvTiers() — "+
			"an environment missing here is one nothing raises the tier of",
			len(config.Floor.EnvTiers), len(estate.DefaultEnvTiers()))
	}
	// The flip side of the same table, and the cost of inheriting it silently: an environment the
	// defaults never heard of is UNKNOWN, not harmless. Resolve refuses it, which is fail-closed
	// but late — the team whose apply fails is the one who finds out.
	if _, ruled := config.Floor.MinTierFor(envNobodyRuled); ruled {
		t.Errorf("environment %q is ruled by a floor that never mentioned it — an unruled "+
			"environment must read as unknown so the caller refuses rather than guesses",
			envNobodyRuled)
	}
}

// Naming one tier's gate must not drop the entries for the tiers the document did not mention.
//
// WHY: toFloor merges onto DefaultGates rather than replacing it. A replace would leave a floor
// carrying one gate, and the deployment above — which names nothing — is one edit away from
// naming exactly one thing.
//
// Distinct from TestADeploymentMayRaiseAGateOnATierTheDefaultTreatsAsHarmless, which reads the
// result through GateFor. GateFor consults DefaultGates for any tier the table is missing, so it
// cannot tell a merged table from a replaced one; only the stored map can.
func TestNamingOneTiersGateLeavesTheOtherTiersEntriesIntact(t *testing.T) {
	config, err := Parse([]byte(withGates(t, litDevOwningTeam)))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	gates := config.Floor.Gates
	if len(gates) != len(estate.DefaultGates()) {
		t.Fatalf("a document naming one tier resolved to %d gates, want %d — the named tier "+
			"REPLACED the ladder instead of being merged onto it",
			len(gates), len(estate.DefaultGates()))
	}
	if got := gates[estate.TierDev]; got != estate.GateOwningTeam {
		t.Errorf("dev gate = %q, want owning-team — the one thing the document asked for", got)
	}
	if got := gates[estate.TierShared]; got != estate.GateOwningTeam {
		t.Errorf("shared gate = %q, want the built-in owning-team", got)
	}
	if got := gates[estate.TierProd]; got != estate.GatePlatform {
		t.Errorf("prod gate = %q, want the built-in platform", got)
	}
}

// Naming one environment's tier must not un-rule the others, and adding an environment must not
// displace the built-in ones.
//
// WHY: an unruled environment is refused at resolve, so a replace here turns "we raised DEV" into
// "SIT and PROD stopped resolving" — and the operator who made that edit raised a gate, so they
// have no reason to suspect they broke two environments they never touched.
func TestNamingOneEnvironmentsTierLeavesTheOthersRuled(t *testing.T) {
	raised, err := Parse([]byte(withFloorSection(t, "envTiers", `{"dev": "shared"}`)))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	// The stored table again, not MinTierFor: the accessor falls back to DefaultEnvTiers, so it
	// cannot tell a merged table from a replaced one.
	if got := raised.Floor.EnvTiers[envDev]; got != estate.TierShared {
		t.Errorf("dev floors at tier %q, want shared — the raise the document asked for did not "+
			"take effect", got)
	}
	for _, untouched := range []struct {
		environment string
		minimum     estate.Tier
	}{
		{envSit, estate.TierShared},
		{envProd, estate.TierProd},
	} {
		got, ruled := raised.Floor.EnvTiers[untouched.environment]
		if !ruled || got != untouched.minimum {
			t.Errorf("after raising dev, environment %q floors at %q (ruled=%t), want %q — "+
				"naming one environment must not un-rule the rest",
				untouched.environment, got, ruled, untouched.minimum)
		}
	}

	// Adding an environment the defaults never heard of is a legitimate edit, and it is an
	// ADDITION: the built-in three come with it.
	added, err := Parse([]byte(withFloorSection(t, "envTiers", `{"uat": "prod"}`)))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	if got, ruled := added.Floor.EnvTiers[envNobodyRuled]; !ruled || got != estate.TierProd {
		t.Errorf("the added environment floors at %q (ruled=%t), want prod", got, ruled)
	}
	if len(added.Floor.EnvTiers) != len(estate.DefaultEnvTiers())+1 {
		t.Errorf("adding one environment resolved to %d rules, want the built-in %d plus one — "+
			"the addition replaced the table rather than extending it",
			len(added.Floor.EnvTiers), len(estate.DefaultEnvTiers()))
	}
}

// flooredAsset is one asset section's answer for one tier: is there an entry at all.
type flooredAsset struct {
	asset   string
	floored bool
}

// Every tier the validator enumerates has limits for every asset the floor governs.
//
// WHY: a tier with no entry for an asset is a tier with NO LIMITS for it. Resolve refuses such a
// tier, which is fail-closed but late — the team whose apply fails is the one who discovers the
// gap. This is the boot-time statement of the same thing, and it is a completeness check rather
// than a value check: it walks the tiers the validator itself walks, so adding a fourth tier to
// `tiers` makes this test demand a floor for it.
func TestEveryTierHasLimitsForEveryAssetTheFloorGoverns(t *testing.T) {
	config, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	floor := config.Floor
	if len(floor.App) == 0 {
		t.Fatal("the resolved floor configures no app runtimes, so the per-runtime half of this " +
			"check would pass by having nothing to check")
	}

	for _, tier := range tiers {
		_, kafka := floor.Kafka[tier]
		_, postgres := floor.Postgres[tier]
		_, harbor := floor.Harbor[tier]
		_, nodePool := floor.Fleet.NodePool[tier]
		governed := []flooredAsset{
			{"kafka", kafka},
			{"postgres", postgres},
			{"harbor", harbor},
			{"fleet nodePool", nodePool},
		}
		// Every runtime is its own floor: an app runtime the document forgot at one tier is a
		// deployment of that shape that nothing bounds.
		for runtime, byTier := range floor.App {
			_, runtimeFloored := byTier[tier]
			governed = append(governed,
				flooredAsset{"app runtime " + string(runtime), runtimeFloored})
		}
		for _, asset := range governed {
			if !asset.floored {
				t.Errorf("tier %q has no %s limits — a tier with no entry is a tier with no "+
					"limits, and it is the team whose apply fails who finds out", tier, asset.asset)
			}
		}
	}
}

// Dropping a tier from an asset section is refused at LOAD, for every section that has tiers.
//
// WHY: the refusal is what makes the completeness above enforceable rather than merely true of
// today's fixture. One case per asset section because each one is a separate lookup in
// validate.go, and a section whose tier check was deleted would still leave this file compiling
// and the other cases passing.
//
// harbor is deliberately absent: TestAMissingTierIsRefused already pins that one, and these are
// the four sections nothing covered.
func TestDroppingATierFromAnAssetSectionIsRefusedAtLoad(t *testing.T) {
	for _, testCase := range []struct {
		asset   string
		tier    estate.Tier
		find    string
		replace string
	}{
		{
			asset:   "kafka",
			tier:    estate.TierProd,
			find:    `"prod":   {"producerBytesPerSec": 104857600,`,
			replace: `"uat":    {"producerBytesPerSec": 104857600,`,
		},
		{
			asset:   "postgres",
			tier:    estate.TierShared,
			find:    `"shared": {"connectionLimit": 50,`,
			replace: `"uat":    {"connectionLimit": 50,`,
		},
		{
			asset:   "app runtime enterprise",
			tier:    estate.TierDev,
			find:    `"dev":    {"replicas": 1, "cpuLimit": "500m"`,
			replace: `"uat":    {"replicas": 1, "cpuLimit": "500m"`,
		},
		{
			asset:   "fleet nodePool",
			tier:    estate.TierShared,
			find:    `"shared": {"minNodes": 1,`,
			replace: `"uat":    {"minNodes": 1,`,
		},
	} {
		t.Run(testCase.asset, func(t *testing.T) {
			_, err := Parse([]byte(mustReplace(t, testCase.find, testCase.replace)))
			if err == nil {
				t.Fatalf("a floor with no %s limits for tier %q was accepted — that tier is now "+
					"unbounded for that asset until an apply fails", testCase.asset, testCase.tier)
			}
			// The operator has to be able to fix it from the message alone: which asset, which
			// tier. "invalid config" sends them reading the whole file.
			for _, want := range []string{testCase.asset, string(testCase.tier)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q, so it does not say what to add: %v",
						want, err)
				}
			}
		})
	}
}

// The revision is the document's BYTES, not its meaning, and it cannot be declared inside the
// floor block either.
//
// WHY: TestTheFloorRevisionIsDerivedFromContentAndCannotBeDeclared pins that the same document
// gives the same revision, that a changed VALUE changes it, and that a top-level "revision" key is
// refused. Two things it leaves open, and both are load-bearing for reading an old decision:
//
//  1. A cosmetic edit — reindenting, reordering — changes the revision too. So two servers running
//     equivalent-but-not-identical files report DIFFERENT revisions, and a diff of the document is
//     the only way to tell "the rules changed" from "someone ran a formatter". Pinning it stops
//     anyone making revision semantic later without noticing what they broke: a floor whose
//     revision skipped whitespace could be edited into a different floor with the same name.
//  2. floorDocument's own comment says there is deliberately no "revision" field THERE. The
//     existing test puts the key at the top level, which a different struct refuses.
func TestTheFloorRevisionIsTheDocumentsBytesNotItsMeaning(t *testing.T) {
	baseline, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	// Twelve hex characters: short enough to sit in an audit column next to a decision, which is
	// the only reason it is truncated at all.
	const revisionLength = 12
	if len(baseline.Floor.Revision) != revisionLength {
		t.Errorf("revision %q is %d characters, want %d — it is read by humans beside a "+
			"decision, and the width is the contract",
			baseline.Floor.Revision, len(baseline.Floor.Revision), revisionLength)
	}
	if strings.Trim(baseline.Floor.Revision, "0123456789abcdef") != "" {
		t.Errorf("revision %q is not lower-case hex, so it is not the content hash it claims "+
			"to be", baseline.Floor.Revision)
	}

	// An edit that changes no limit at all still changes the revision.
	reformatted, err := Parse([]byte(mustReplace(t, litOwnershipEmpty, "\n  "+litOwnershipEmpty)))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	if reformatted.Floor.Revision == baseline.Floor.Revision {
		t.Error("a reformatted document kept the same revision — the revision would then be a " +
			"claim about MEANING, and two different floors could share one name")
	}

	// Declared, a revision can be forgotten on an edit, and a floor claiming a revision it is not
	// is worse than no revision at all. The unknown-field refusal is what prevents it.
	_, err = Parse([]byte(withFloorSection(t, "revision", `"hand-written"`)))
	if err == nil {
		t.Fatal("a floor block declared its own revision and was accepted — every decision under " +
			"it would name a floor nobody can reproduce")
	}
	if !strings.Contains(err.Error(), "revision") {
		t.Errorf("the refusal does not name the offending field: %v", err)
	}
}

// A document that PARSES but fails validation leaves the previous floor serving.
//
// WHY: TestABadFileOnReloadLeavesThePreviousFloorServing drives a truncated document, which the
// JSON decoder rejects before any validator runs. The mistake an operator actually makes is a
// well-formed document with a bad VALUE — a zero limit, a lowered prod gate — and that travels a
// different path through Load. Validate-then-swap has to hold on that path too, or the reload that
// un-governs production is the one that looked like valid JSON.
func TestADocumentThatParsesButFailsValidationLeavesThePreviousFloorServing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "floor.json")
	if err := os.WriteFile(path, []byte(validDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	live, err := OpenLive(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	serving := live.Floor().Revision

	// Syntactically perfect, and it lowers prod from platform to no gate at all.
	if err := os.WriteFile(path, []byte(withGates(t, `{"prod": "none"}`)), 0o600); err != nil {
		t.Fatal(err)
	}
	inForce, err := live.Reload()
	if err == nil {
		t.Fatal("a document un-gating production reloaded cleanly — a hot reload became the way " +
			"round the floor")
	}
	if !strings.Contains(err.Error(), "cannot lower the floor") {
		t.Errorf("the reload error does not say why it was refused: %v", err)
	}
	if inForce.Floor.Revision != serving {
		t.Errorf("reload reported revision %q, want the previous %q — an operator must be told "+
			"what is actually governing, not what they hoped would be",
			inForce.Floor.Revision, serving)
	}
	if live.Floor().Revision != serving {
		t.Errorf("the live revision is %q after a FAILED reload, want %q — the swap happened "+
			"despite the validation error", live.Floor().Revision, serving)
	}
	if got := live.Floor().GateFor(estate.TierProd); got != estate.GatePlatform {
		t.Errorf("prod gate = %q after a refused reload, want platform — the refused document "+
			"reached the running floor anyway", got)
	}
}

// A config built in code says there is nothing to reload, and keeps serving what it has.
//
// WHY: NewLive exists for an embedded deployment or a test that builds its floor in code, and it
// has no file behind it. Reload on one must be distinguishable from a FAILED reload — those are
// opposite situations for an operator — and it must not leave a caller holding an empty floor. The
// function has no other test and no production caller yet, which is exactly when a later refactor
// turns "nothing to reload" into a zero Config.
func TestAConfigBuiltInCodeReportsNothingToReloadAndKeepsServing(t *testing.T) {
	built, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf(litGatesTestParse, err)
	}
	live := NewLive(built)
	if live.Path() != "" {
		t.Errorf("an in-code config reports path %q, want empty — a reload would re-read a file "+
			"nobody wrote", live.Path())
	}

	inForce, err := live.Reload()
	if !errors.Is(err, errNoPath) {
		t.Fatalf("reload of an in-code config returned %v, want errNoPath — a caller cannot tell "+
			"\"nothing to reload\" from \"the reload failed\"", err)
	}
	if inForce.Floor.Revision != built.Floor.Revision {
		t.Errorf("reload returned revision %q, want the config that is still serving (%q)",
			inForce.Floor.Revision, built.Floor.Revision)
	}
	if got := live.Floor().GateFor(estate.TierProd); got != estate.GatePlatform {
		t.Errorf("prod gate = %q after a no-op reload, want platform — the floor was emptied by "+
			"a reload that did nothing", got)
	}
}

// An absent config path is refused at boot rather than falling back to the built-in floor.
//
// WHY: DefaultFloor is a starting point for AUTHORING the document, never a fallback to govern
// under — a deployment that boots on it is enforcing limits nobody in that deployment chose, and
// it boots green, so nothing says so. TestAnAbsentConfigPathIsRefused pins Load(""); this pins
// OpenLive(""), which is the constructor serve.go actually calls and therefore the place a
// well-meaning "just use the default" would be added.
func TestOpenLiveWithNoPathRefusesRatherThanFallingBackToTheBuiltInFloor(t *testing.T) {
	live, err := OpenLive("")
	if err == nil {
		t.Fatal("OpenLive(\"\") returned a running config for a path nobody set — the floor came " +
			"from the binary and no operator chose it")
	}
	if live != nil {
		t.Error("OpenLive returned a *Live alongside its error — a caller that only checks for a " +
			"nil pointer would serve a zero floor, which bounds nothing")
	}
	// This is the one error a deployment meets at boot with nothing else on screen, so it has to
	// carry the fix: the variable to set, and that the built-in floor is not an alternative.
	for _, want := range []string{"MANTLEKEEP_ESTATE_CONFIG", "not a fallback"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the boot refusal never mentions %q, so an operator reading it cannot act "+
				"on it: %v", want, err)
		}
	}
}
