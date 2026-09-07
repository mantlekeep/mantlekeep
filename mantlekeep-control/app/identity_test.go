package app

import (
	"context"
	"strings"
	"testing"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// Which resolver a deployment gets, and what it will admit about itself. Both matter to the
// one screen people read before granting somebody access: a directory that cannot be listed
// must not be listable, and a directory that was ASSERTED must not claim to be a directory of
// record.

// The dev tier can be listed, and says it is not authoritative. Both halves are load-bearing:
// a fixture nobody can enumerate makes the page useless, and one that claims to be the staff
// list makes it dangerous.
func TestTheDevDirectoryCanBeListedAndAdmitsItIsNotOfRecord(t *testing.T) {
	resolver := BuildIdentity()

	lister, ok := resolver.(mantlekeep.SubjectLister)
	if !ok {
		t.Fatalf("%T cannot be listed, so a permissions surface can show nobody in the dev tier", resolver)
	}
	subjects, err := lister.Subjects(context.Background())
	if err != nil {
		t.Fatalf("listing the dev directory: %v", err)
	}
	if len(subjects) == 0 {
		t.Fatal("the dev directory listed nobody")
	}
	// Sorted, so the same directory renders the same page twice running.
	for i := 1; i < len(subjects); i++ {
		if subjects[i-1].ID > subjects[i].ID {
			t.Fatalf("the listing is not sorted by id, so two reads of an unchanged directory "+
				"differ for no reason anybody can act on: %v", subjects)
		}
	}

	describer, ok := resolver.(mantlekeep.DirectoryDescriber)
	if !ok {
		t.Fatalf("%T does not say what it is, so nothing can tell a fixture from a directory", resolver)
	}
	described := describer.DescribeDirectory()
	if described.Authoritative {
		t.Error("the dev directory claims to be a directory of record — every identity in it " +
			"was compiled in, and a surface that showed it as the staff list would be believed")
	}
	if described.Note == "" || described.Name == "" {
		t.Errorf("a non-authoritative directory must name itself and say what not to conclude: %+v", described)
	}
}

// The gateway resolver must NOT be listable. It holds a group→role table, never people: the
// people are in the IdP behind the gateway. A Subjects method here could only return groups
// dressed as users, which is a plausible wrong answer — the worst kind on this screen.
func TestTheGatewayResolverRefusesToBeListedRatherThanInventAPopulation(t *testing.T) {
	t.Setenv("MANTLEKEEP_AUTH", "proxy")
	t.Setenv("MANTLEKEEP_GROUP_ROLES", "platform-architects=L1-Architect;ops-operators=L2-Operator")

	resolver := BuildIdentity()
	if _, listable := resolver.(mantlekeep.SubjectLister); listable {
		t.Fatal("the gateway resolver offers to list its subjects — it has none to list, so " +
			"whatever it returned would be invented")
	}
	describer, ok := resolver.(mantlekeep.DirectoryDescriber)
	if !ok {
		t.Fatal("the gateway resolver does not say what it is")
	}
	if described := describer.DescribeDirectory(); !described.Authoritative {
		t.Errorf("the gateway's identities come from a real IdP and it reports itself as not "+
			"of record: %+v", described)
	}
}

// DevSubjectsEnv lets a deployment name its own dev population — and the result is still
// asserted, not looked up, so it must still declare itself not of record.
func TestDevSubjectsConfiguresTheDirectoryAndIsStillNotOfRecord(t *testing.T) {
	t.Setenv(DevSubjectsEnv, "ops-dana=L2-Operator;robot=AI-Agent")

	resolver := BuildIdentity()
	lister, ok := resolver.(mantlekeep.SubjectLister)
	if !ok {
		t.Fatalf("%T cannot be listed", resolver)
	}
	subjects, err := lister.Subjects(context.Background())
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(subjects) != 2 {
		t.Fatalf("the configured directory holds %d subjects, want the 2 named: %v", len(subjects), subjects)
	}
	byID := map[string]mantlekeep.Subject{}
	for _, subject := range subjects {
		byID[subject.ID] = subject
	}
	dana, named := byID["ops-dana"]
	if !named || len(dana.Roles) != 1 || dana.Roles[0] != mantlekeep.RoleOperator {
		t.Errorf("ops-dana resolved to %+v, want the configured L2-Operator", dana)
	}
	// The AI guardrails must arm the same way whichever resolver produced the subject, or a
	// service account is de-fanged by being declared in the other one.
	if robot := byID["robot"]; !robot.IsAI {
		t.Errorf("a subject configured as AI-Agent is not flagged IsAI: %+v", robot)
	}
	if described := resolver.(mantlekeep.DirectoryDescriber).DescribeDirectory(); described.Authoritative {
		t.Error("a directory named in configuration claims to be a directory of record")
	}

	// And it must still resolve, because a list nothing can resolve against is decoration.
	resolved, err := resolver.Resolve(context.Background(), mantlekeep.ExternalIdentity{ID: "ops-dana"})
	if err != nil || resolved.ID != "ops-dana" {
		t.Errorf("resolving a configured subject: %+v %v", resolved, err)
	}
	// The seeded demo set must be GONE — a configured directory that still answered about the
	// compiled-in names would show people the operator never named.
	if _, err := resolver.Resolve(context.Background(), mantlekeep.ExternalIdentity{ID: "dev-alice"}); err == nil {
		t.Error("the configured directory still resolves a compiled-in demo subject, so the " +
			"configuration ADDED to the fixture instead of replacing it")
	}
}

// The configured directory and the seeded one must describe themselves DIFFERENTLY. A reader
// who cannot see which of the two they are looking at cannot tell a stale demo from their own
// configuration.
func TestTheConfiguredAndSeededDirectoriesDoNotDescribeThemselvesTheSameWay(t *testing.T) {
	seeded := BuildIdentity().(mantlekeep.DirectoryDescriber).DescribeDirectory()

	t.Setenv(DevSubjectsEnv, "ops-dana=L2-Operator")
	configured := BuildIdentity().(mantlekeep.DirectoryDescriber).DescribeDirectory()

	if seeded.Name == configured.Name {
		t.Fatalf("both dev directories say %q, so nothing on the page distinguishes a demo "+
			"fixture from this deployment's own configuration", seeded.Name)
	}
	if seeded.Authoritative || configured.Authoritative {
		t.Error("a dev directory claimed to be a directory of record")
	}
}

// A chain must refuse to be listed unless EVERY layer can be. Listing the half that can be
// listed produces a page that looks complete and is missing exactly the population the other
// layer holds — and nothing on it says so.
func TestALayeredDirectoryRefusesToListWhenAnyLayerCannot(t *testing.T) {
	// The mesh transport layers a service rolemap (not listable) in front of the human
	// directory (listable), which is the composition BuildIdentity produces.
	t.Setenv("MANTLEKEEP_TRANSPORT", "mesh")
	t.Setenv("MANTLEKEEP_SERVICE_ROLES", "spiffe://cluster.local/ns/demo/sa/svc-a=L3-Consumer")

	resolver := BuildIdentity()
	lister, ok := resolver.(mantlekeep.SubjectLister)
	if !ok {
		t.Fatal("this composition is not a chain, so the refusal it is supposed to make " +
			"cannot be observed — the test would pass having proved nothing")
	}
	subjects, err := lister.Subjects(context.Background())
	if err == nil {
		t.Fatalf("a chain with an unlistable layer returned %d subjects as though the list were "+
			"complete: %v", len(subjects), subjects)
	}
	if !strings.Contains(err.Error(), "cannot be listed") {
		t.Errorf("the refusal does not say the directory cannot be listed: %q", err)
	}
	if len(subjects) != 0 {
		t.Errorf("a refused listing still returned %d subjects", len(subjects))
	}

	// And the chain's self-description is authoritative only if every layer is. The gateway
	// half is of record and the dev half is not, so the whole answer must not be.
	described := resolver.(mantlekeep.DirectoryDescriber).DescribeDirectory()
	if described.Authoritative {
		t.Error("a chain containing an ASSERTED dev directory reports itself as a directory " +
			"of record — one asserted layer makes the whole answer asserted")
	}
	if described.Note == "" {
		t.Error("a non-authoritative chain must say what a reader must not conclude from it")
	}
}
