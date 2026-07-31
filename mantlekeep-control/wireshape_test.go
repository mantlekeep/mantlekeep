package mantlekeep

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPolicyInputWireShape PINS the JSON contract a policy plugin (wasm/gRPC, in a
// host's own language) receives. A field rename/removal fails this test — that is a
// BREAKING change, so it must come with a ContractVersion MAJOR bump. The golden
// keys below are the frozen wire shape.
func TestPolicyInputWireShape(t *testing.T) {
	in := PolicyInput{
		Subject: PolicySubject{ID: "u", Roles: []Role{RoleOperator}, IsAI: false, Attrs: map[string]string{"dept": "platform"}},
		Intent:  PolicyIntent{Action: "job.promote", Resource: "r", Requester: "alice", Env: "PROD", Goal: "ship", Params: map[string]any{"count": 1000}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	// The frozen dotted key paths downstream policies read.
	for _, key := range []string{
		`"subject"`, `"id"`, `"roles"`, `"is_ai"`, `"attrs"`,
		`"intent"`, `"action"`, `"resource"`, `"requester"`, `"env"`, `"goal"`, `"params"`,
	} {
		if !strings.Contains(got, key) {
			t.Errorf("PolicyInput wire shape lost %s — BREAKING; bump ContractVersion (MAJOR). got: %s", key, got)
		}
	}
	// Round-trip must preserve the values (a wasm policy marshals it back).
	var back PolicyInput
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Subject.ID != "u" || back.Intent.Env != "PROD" || back.Intent.Action != "job.promote" {
		t.Fatalf("PolicyInput round-trip mismatch: %+v", back)
	}
}
