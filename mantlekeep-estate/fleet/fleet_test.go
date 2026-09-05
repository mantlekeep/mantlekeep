package fleet_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
	"github.com/mantlekeep/mantlekeep/mantlekeep-estate/fleet"
)

const registry = `{"clusters":[
  {"name":"dev-app-region-a-1","provider":"gke","region":"region-a","env":"dev","purpose":"app","residency":"region-a"},
  {"name":"dev-app-region-b-1","provider":"provider-c","region":"region-b","env":"dev","purpose":"app","residency":"region-b"}]}`

func TestTheRegistryLoads(t *testing.T) {
	clusters, err := fleet.Parse([]byte(registry))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("want 2 clusters, got %d", len(clusters))
	}
	// Reachability is MEASURED, not declared. A registry that could assert a cluster is up
	// would be a file making a claim about the world.
	for _, cluster := range clusters {
		if cluster.Reachable {
			t.Fatalf("%s loaded as reachable — only an observation may say that", cluster.Name)
		}
	}
}

// A misspelled residency silently ignored is worse than a missing one: the operator believes
// the jurisdiction is recorded. On this document that mistake places data in the wrong country.
func TestAnUnknownFieldIsRefused(t *testing.T) {
	_, err := fleet.Parse([]byte(`{"clusters":[
	  {"name":"c","env":"dev","residency":"region-a","residncy":"region-b"}]}`))
	if err == nil {
		t.Fatal("a misspelled field was ignored — the operator would believe it took effect")
	}
}

// A cluster whose jurisdiction is unknown must never be a candidate, and saying so at load time
// beats discovering it after data has landed.
func TestAClusterWithNoResidencyIsRefusedAtLoad(t *testing.T) {
	_, err := fleet.Parse([]byte(`{"clusters":[{"name":"c","env":"dev"}]}`))
	if err == nil {
		t.Fatal("a cluster with no residency loaded")
	}
	if !strings.Contains(err.Error(), "jurisdiction") {
		t.Fatalf("the refusal must say why; got %v", err)
	}
}

// Two entries with one name means one is invisible, and which wins is map-order luck — a
// placement decided by chance.
func TestADuplicateClusterIsRefused(t *testing.T) {
	_, err := fleet.Parse([]byte(`{"clusters":[
	  {"name":"c","env":"dev","residency":"region-a"},{"name":"c","env":"dev","residency":"region-b"}]}`))
	if err == nil {
		t.Fatal("a duplicate cluster name loaded")
	}
}

func TestReachabilityIsAppliedSeparately(t *testing.T) {
	clusters, _ := fleet.Parse([]byte(registry))
	marked := fleet.MarkReachable(clusters, map[string]bool{"dev-app-region-a-1": true})

	for _, cluster := range marked {
		want := cluster.Name == "dev-app-region-a-1"
		if cluster.Reachable != want {
			t.Fatalf("%s reachable=%v, want %v", cluster.Name, cluster.Reachable, want)
		}
	}
}

// KSM in the Prometheus text format, parsed with the standard library.
func ksmServer(allocatable, requested string) *httptest.Server {
	body := `# HELP kube_node_status_allocatable
kube_node_status_allocatable{node="n1",resource="memory",unit="byte"} ` + allocatable + `
kube_node_status_allocatable{node="n1",resource="cpu",unit="core"} 8
kube_pod_container_resource_requests{node="n1",resource="memory",unit="byte"} ` + requested + `
kube_pod_container_resource_requests{node="n1",resource="cpu",unit="core"} 6
`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
}

func TestFreeMemoryIsReadFromKSM(t *testing.T) {
	server := ksmServer("1000", "250")
	defer server.Close()

	reports, unread := fleet.NewKSM(map[string]string{"c": server.URL}).Read(context.Background())
	if len(unread) != 0 {
		t.Fatalf("nothing should be unread; got %v", unread)
	}
	if len(reports) != 1 || reports[0].AllocatablePct < 0.74 || reports[0].AllocatablePct > 0.76 {
		t.Fatalf("want ~0.75 free, got %+v", reports)
	}
}

// THE trap. Zero means FULL, which excludes a cluster from placement entirely — so a metrics
// outage would silently stop apps being placed anywhere, and the cause would look like a
// capacity problem rather than a monitoring one.
func TestAnUnreachableKSMIsUnknownNotFull(t *testing.T) {
	server := ksmServer("1000", "250")
	server.Close() // dead before we read it

	reports, unread := fleet.NewKSM(map[string]string{"c": server.URL}).Read(context.Background())

	for _, report := range reports {
		if report.Cluster == "c" {
			t.Fatalf("a dead KSM produced a capacity report of %v — unknown must be OMITTED, "+
				"because zero would read as full and remove the cluster from placement",
				report.AllocatablePct)
		}
	}
	if len(unread) != 1 || unread[0] != "c" {
		t.Fatalf("the cluster must be reported as unread; got %v", unread)
	}
}

// And unknown must still be placeable — ranked last, never excluded.
func TestAClusterWithUnknownCapacityIsStillACandidate(t *testing.T) {
	clusters, _ := fleet.Parse([]byte(registry))
	placer := estate.NewPlacer(fleet.MarkReachable(clusters, map[string]bool{"dev-app-region-a-1": true}))
	// no capacity supplied at all

	decision, err := placer.Place(
		estate.Placement{Env: "dev", Purpose: "app", Residency: "region-a"}, "")
	if err != nil {
		t.Fatalf("unknown capacity must not block placement: %v", err)
	}
	if decision.Cluster != "dev-app-region-a-1" {
		t.Fatalf("got %q", decision.Cluster)
	}
}

// No allocatable memory is a metrics problem, not an empty cluster. Reporting it as free would
// advertise infinite room on a cluster nobody measured.
func TestNoAllocatableReportedIsAnError(t *testing.T) {
	server := ksmServer("0", "0")
	defer server.Close()

	reports, unread := fleet.NewKSM(map[string]string{"c": server.URL}).Read(context.Background())
	if len(reports) != 0 {
		t.Fatalf("a cluster reporting no allocatable memory must not be reported as free; got %+v", reports)
	}
	if len(unread) != 1 {
		t.Fatal("it must be listed as unread")
	}
}

// Over-committed is full, and honestly so — never negative.
func TestOvercommittedReadsAsFullNotNegative(t *testing.T) {
	server := ksmServer("1000", "1500")
	defer server.Close()

	reports, _ := fleet.NewKSM(map[string]string{"c": server.URL}).Read(context.Background())
	if len(reports) != 1 || reports[0].AllocatablePct != 0 {
		t.Fatalf("want 0 free, got %+v", reports)
	}
}
