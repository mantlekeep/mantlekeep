package provider

import "testing"

// A greeter capability with two implementations, to test the generic registry.
type greeter interface{ hello() string }
type en struct{}
type fr struct{}

func (en) hello() string { return "hello" }
func (fr) hello() string { return "bonjour" }

func TestRegistryHoldsManyProviders(t *testing.T) {
	r := New[greeter]("greeter").Register("en", en{}).Register("fr", fr{})
	if got := r.Names(); len(got) != 2 {
		t.Fatalf("want 2 providers, got %v", got)
	}
	g, err := r.Get("fr")
	if err != nil || g.hello() != "bonjour" {
		t.Fatalf("get fr: %v / %q", err, g.hello())
	}
}

func TestRegistryUnknownProvider(t *testing.T) {
	r := New[greeter]("greeter").Register("en", en{})
	if _, err := r.Get("de"); err == nil {
		t.Fatal("expected error for unregistered provider")
	}
}

// Config bindings route each role to a specific provider — all live at once.
func TestBindingsRouteRolesToProviders(t *testing.T) {
	r := New[greeter]("greeter").Register("en", en{}).Register("fr", fr{})
	b := Bindings{"formal": "fr", "casual": "en"}

	formal, err := r.For(b, "formal")
	if err != nil || formal.hello() != "bonjour" {
		t.Fatalf("formal → fr failed: %v / %q", err, formal.hello())
	}
	casual, _ := r.For(b, "casual")
	if casual.hello() != "hello" {
		t.Fatalf("casual → en failed: %q", casual.hello())
	}

	// Re-binding a role by config alone switches the backend — no code change.
	b["casual"] = "fr"
	if c, _ := r.For(b, "casual"); c.hello() != "bonjour" {
		t.Fatalf("rebinding casual → fr failed: %q", c.hello())
	}
}

func TestBindingMissingRole(t *testing.T) {
	r := New[greeter]("greeter").Register("en", en{})
	if _, err := r.For(Bindings{}, "nope"); err == nil {
		t.Fatal("expected error for unbound role")
	}
}
