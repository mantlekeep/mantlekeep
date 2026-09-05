package estate

import "testing"

// The gap that shipped: Promotion set Digest, the adapter required Image, and nothing tested
// the two TOGETHER — so "build once, promote the digest" was modelled, unit-tested, and could
// not deploy. A test of one entry point is not a test of the behaviour.
func TestAPromotionCarriesEnoughToActuallyDeploy(t *testing.T) {
	promotion := Promotion{
		Team: "payments", App: "api", Repository: "harbor/payments/api", Digest: "sha256:9c4f",
		To:   Slot{Cluster: "prod", Namespace: "payments-v2", Name: "api"},
		Tier: TierProd,
	}

	change, err := promotion.AsChange(DefaultFloor(), RuntimeEnterprise)
	if err != nil {
		t.Fatalf("as change: %v", err)
	}

	// What an adapter needs to build a container. Without a reference it cannot deploy, and a
	// digest alone is not a pullable reference.
	if change.Image == "" {
		t.Fatal("a promotion produced no image reference — the adapter cannot deploy it, so " +
			"the promotion path is modelled but dead")
	}
	if change.Digest != promotion.Digest {
		t.Fatalf("the approved digest must survive; got %q", change.Digest)
	}
	if change.Slot.Namespace != "payments-v2" {
		t.Fatalf("the target slot must survive; got %+v", change.Slot)
	}
}
