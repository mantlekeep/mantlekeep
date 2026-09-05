package estate

import (
	"strings"
	"testing"
	"time"
)

// The sealed floor, tested from the outside: every one of these is a way a team might try to
// widen its own limits, and each must fail at PARSE time rather than be ignored at apply time.
// Silently dropping an unrecognised field is how a team believes it set a limit that was never
// applied — the failure is invisible precisely because it looks like success.
func TestAManifestCannotNameItsOwnLimits(t *testing.T) {
	for _, attempt := range []struct {
		what     string
		document string
	}{
		{"a kafka quota", `{"team":"payments","owns":"payments","kafka":{"quotaBytesPerSec":999999999}}`},
		{"a kafka retention", `{"team":"payments","owns":"payments","kafka":{"retention":"3650d"}}`},
		{"a postgres connection limit", `{"team":"payments","owns":"payments","postgres":[{"cluster":"c","database":"d","connectionLimit":500}]}`},
		{"a robot expiry", `{"team":"payments","owns":"payments","harbor":{"project":"payments","robotExpiry":"never"}}`},
		{"app replicas", `{"team":"payments","owns":"payments","apps":[{"name":"api","runtime":"enterprise","image":"h/p/api","placement":{"env":"dev","purpose":"app","residency":"region-a"},"replicas":50}]}`},
		{"a per-item limit", `{"team":"payments","owns":"payments","kafka":{"topics":[{"name":"orders","retention":"999d"}]}}`},
	} {
		t.Run(attempt.what, func(t *testing.T) {
			if _, err := ParseManifest([]byte(attempt.document)); err == nil {
				t.Fatalf("a manifest naming %s was ACCEPTED — the floor is not sealed", attempt.what)
			}
		})
	}
}

// Raising is the whole extend/override mechanism; lowering would let a team declare a
// production resource to be a playground and take its gate away.
func TestAnItemMayRaiseItsTierButNeverLowerIt(t *testing.T) {
	raise := `{"team":"payments","owns":"payments","tier":"dev",
	           "kafka":{"topics":[{"name":"settlements","tier":"prod"}]}}`
	if _, err := ParseManifest([]byte(raise)); err != nil {
		t.Fatalf("raising a single item to prod must be allowed: %v", err)
	}

	lower := `{"team":"payments","owns":"payments","tier":"prod",
	           "kafka":{"topics":[{"name":"scratch","tier":"dev"}]}}`
	_, err := ParseManifest([]byte(lower))
	if err == nil {
		t.Fatal("an item lowered its tier below the manifest's and was ACCEPTED")
	}
	if !strings.Contains(err.Error(), "never lower") {
		t.Fatalf("the refusal must say why; got %q", err)
	}
}

// A tag is a moving pointer: approving one approves whatever it points at tomorrow. That is
// exactly the gap between "they approved deploy v7" and "v8 shipped".
func TestAnAppImageMustNotCarryATagOrDigest(t *testing.T) {
	for _, image := range []string{"harbor/payments/api:latest", "harbor/payments/api@sha256:abc"} {
		document := `{"team":"payments","owns":"payments","apps":[{"name":"api",` +
			`"runtime":"enterprise","image":"` + image + `","placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`
		_, err := ParseManifest([]byte(document))
		if err == nil {
			t.Fatalf("image %q was accepted — the approval could not bind to a digest", image)
		}
		// Assert WHY. A test that accepts any error passes for the wrong reason the moment a
		// new required field is added, and then it is guarding nothing.
		if !strings.Contains(err.Error(), "must not carry a tag or digest") {
			t.Fatalf("image %q was refused for the wrong reason: %v", image, err)
		}
	}
}

// The default shape: four lines, no gate, and still floored.
func TestTheDefaultManifestIsFourLinesAndNeedsNoGate(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	                                 "kafka":{"topics":["orders"]}}`))
	if err != nil {
		t.Fatalf("the simplest manifest must parse: %v", err)
	}

	desired, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
		Residency: "region-a", Reachable: true}}), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if desired.NeedsGate() {
		t.Fatal("a dev-tier footprint asked for human attention — gating a playground is the " +
			"ceremony that makes the golden path slower than the bypass")
	}

	// Floored anyway. Nobody asked for these, which is the point.
	var topic DesiredItem
	for _, change := range desired.Changes {
		if change.Kind == "topic" {
			topic = change
		}
	}
	limits, ok := topic.Limits.(KafkaLimits)
	if !ok {
		t.Fatalf("a topic came back without kafka limits: %#v", topic.Limits)
	}
	if limits.Retention != 7*24*time.Hour || limits.ProducerBytesPerSec == 0 {
		t.Fatalf("a dev topic must be floored whether or not anyone asked; got %+v", limits)
	}
	if topic.Name != "payments.orders" {
		t.Fatalf("a topic must be prefixed with the namespace the team owns; got %q", topic.Name)
	}
}

// One prod item pulls in a gate for that item alone — nobody has to choose between
// "everything is a playground" and "everything needs approval".
func TestOneProdItemGatesOnlyItself(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "kafka":{"topics":["orders",{"name":"settlements","tier":"prod"}]}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	desired, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
		Residency: "region-a", Reachable: true}}), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	gates := map[string]Gate{}
	for _, change := range desired.Changes {
		gates[change.Name] = change.Gate
	}
	if gates["payments.orders"] != GateNone {
		t.Fatalf("the dev topic must stay instant; got %q", gates["payments.orders"])
	}
	if gates["payments.settlements"] != GatePlatform {
		t.Fatalf("the prod topic must reach the platform gate; got %q", gates["payments.settlements"])
	}
	if !desired.NeedsGate() {
		t.Fatal("a change set containing a prod item must need a gate")
	}
}

// Two apps sharing a database, and one app with a second cluster — the messy real cases that
// a list of bindings handles without a special case.
func TestPostgresBindingsHandleSharedAndMultiCluster(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev","postgres":[
	    {"cluster":"shared-oltp","database":"appdb","schema":"payments","readers":["risk"]},
	    {"cluster":"payments-analytics","database":"analytics","schema":"main","tier":"shared"}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	desired, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
		Residency: "region-a", Reachable: true}}), nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	var schemas []DesiredItem
	for _, change := range desired.Changes {
		if change.Asset == "postgres" {
			schemas = append(schemas, change)
		}
	}
	if len(schemas) != 2 {
		t.Fatalf("want 2 postgres bindings, got %d", len(schemas))
	}
	if schemas[0].Gate != GateNone || schemas[1].Gate != GateOwningTeam {
		t.Fatalf("each binding carries its OWN tier; got %q and %q", schemas[0].Gate, schemas[1].Gate)
	}
	if len(schemas[0].Readers) != 1 || schemas[0].Readers[0] != "risk" {
		t.Fatalf("a cross-team read must survive resolve as an explicit entry; got %v", schemas[0].Readers)
	}
}

// A runtime is checked in TWO places, on purpose: the shape of the name here, and whether it
// EXISTS at resolve, where the floor is the authority. A hardcoded list in the parser would be
// a second place to edit, and the two would disagree the first time only one was updated —
// accepting a runtime no adapter can deploy.
func TestAnAppMustDeclareAWellFormedRuntime(t *testing.T) {
	document := `{"team":"payments","owns":"payments","apps":[{"name":"api","image":"h/p/api","placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`
	_, err := ParseManifest([]byte(document))
	if err == nil {
		t.Fatal("an app with no runtime was accepted — the platform would have to guess")
	}
	if !strings.Contains(err.Error(), "not a valid runtime name") {
		t.Fatalf("the refusal must say what is wrong; got %v", err)
	}
}

// The floor enumerates the runtimes a deployment serves. Adding one is config, not a release —
// but a runtime with no floor is refused rather than defaulted.
func TestAnUnconfiguredRuntimeIsRefusedByTheFloor(t *testing.T) {
	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"api","runtime":"spark","image":"h/p/api","placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`))
	if err != nil {
		t.Fatalf("a well-formed runtime must PARSE — existence is the floor's answer: %v", err)
	}

	if _, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
		Residency: "region-a", Reachable: true}}), nil); err == nil {
		t.Fatal("an unconfigured runtime resolved — no adapter could deploy it")
	} else if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("the refusal must name the cause; got %v", err)
	}
}

// Adding a runtime is CONFIG. This proves it: a floor that knows "spark" resolves it, with no
// code change anywhere.
func TestAddingARuntimeIsConfigNotARelease(t *testing.T) {
	floor := DefaultFloor()
	floor.App["spark"] = map[Tier]AppLimits{
		TierDev: {Replicas: 1, CPULimit: "2", MemoryMiB: 4096},
	}

	m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
	    "apps":[{"name":"etl","runtime":"spark","image":"h/p/etl","placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	desired, err := ResolveWith(m, floor, NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
		Residency: "region-a", Reachable: true}}), nil)
	if err != nil {
		t.Fatalf("a configured runtime must resolve with no code change: %v", err)
	}
	for _, change := range desired.Changes {
		if change.Asset == "app" && change.Runtime != "spark" {
			t.Fatalf("the runtime must survive resolve; got %q", change.Runtime)
		}
	}
}

// An analytics app holds a dataframe in memory; an enterprise service scales out. One
// shared limits table would starve the first and over-provision the second.
func TestTheFloorDiffersByRuntime(t *testing.T) {
	parse := func(runtime string) DesiredItem {
		t.Helper()
		m, err := ParseManifest([]byte(`{"team":"payments","owns":"payments","tier":"dev",
		    "apps":[{"name":"api","runtime":"` + runtime + `","image":"h/p/api","placement":{"env":"dev","purpose":"app","residency":"region-a"}}]}`))
		if err != nil {
			t.Fatalf("parse %s: %v", runtime, err)
		}
		desired, err := ResolveWith(m, DefaultFloor(), NewPlacer([]Cluster{{Name: "dev-app-region-a-1", Env: "dev", Purpose: "app",
			Residency: "region-a", Reachable: true}}), nil)
		if err != nil {
			t.Fatalf("resolve %s: %v", runtime, err)
		}
		for _, change := range desired.Changes {
			if change.Asset == "app" {
				return change
			}
		}
		t.Fatalf("no app change resolved for %s", runtime)
		return DesiredItem{}
	}

	enterprise := parse("enterprise").Limits.(AppLimits)
	analytics := parse("analytics").Limits.(AppLimits)

	if analytics.MemoryMiB <= enterprise.MemoryMiB {
		t.Fatalf("an analytics app needs more memory than a service; got analytics=%d enterprise=%d",
			analytics.MemoryMiB, enterprise.MemoryMiB)
	}
	if parse("analytics").Runtime != "analytics" {
		t.Fatal("the runtime must survive resolve — the platform needs it to deploy")
	}
}
