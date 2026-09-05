package kafkagrant

import (
	"errors"
	"strings"
	"testing"
)

func TestCoversMirrorsPrefixedSemantics(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		target string
		want   bool
	}{
		{"inside the namespace", "payments.", "payments.settlement.v1", true},
		{"equal to the prefix", "payments.", "payments.", true},
		{"outside the namespace", "payments.", "ledger.settlement.v1", false},
		{"prefix is a substring, not a prefix", "payments.", "eu.payments.v1", false},
		{"neighbouring namespace sharing a stem", "payments.", "payments-archive.v1", false},
		{"empty prefix covers nothing here, even though HasPrefix would say yes", "", "anything.at.all", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := covers(testCase.prefix, testCase.target); got != testCase.want {
				t.Fatalf("covers(%q, %q) = %v, want %v", testCase.prefix, testCase.target, got, testCase.want)
			}
		})
	}
}

func TestValidatePrefixRefusesUnboundedPrefixes(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"empty — would cover the whole cluster", ""},
		{"bare wildcard", "*"},
		{"embedded wildcard", "payments.*"},
		{"illegal character for a Kafka resource name", "payments/settlement"},
		{"whitespace", "payments settlement"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validatePrefix(testCase.prefix)
			if err == nil {
				t.Fatalf("validatePrefix(%q) = nil, want a refusal", testCase.prefix)
			}
			if !errors.Is(err, ErrInvalidGrant) {
				t.Fatalf("validatePrefix(%q) error = %v, want it to wrap ErrInvalidGrant", testCase.prefix, err)
			}
		})
	}
}

func TestValidatePrefixAcceptsAWellFormedNamespace(t *testing.T) {
	for _, prefix := range []string{"payments.", "payments_eu-", "a"} {
		if err := validatePrefix(prefix); err != nil {
			t.Fatalf("validatePrefix(%q) = %v, want nil", prefix, err)
		}
	}
}

func TestValidateResourceNameEnforcesKafkasLimits(t *testing.T) {
	cases := []struct {
		name  string
		topic string
	}{
		{"empty", ""},
		{"reserved dot", "."},
		{"reserved double dot", ".."},
		{"too long for Kafka", strings.Repeat("a", maxResourceNameLength+1)},
		{"illegal character", "payments.settlement:v1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateResourceName(testCase.topic, "topic"); err == nil {
				t.Fatalf("validateResourceName(%q) = nil, want a refusal", testCase.topic)
			}
		})
	}
	if err := validateResourceName(strings.Repeat("a", maxResourceNameLength), "topic"); err != nil {
		t.Fatalf("a name at exactly Kafka's limit must be accepted, got %v", err)
	}
}

func TestPrincipalSplitsTypeFromName(t *testing.T) {
	principal := Principal("User:svc-payments")
	if err := principal.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	if got := principal.Type(); got != "User" {
		t.Fatalf("Type() = %q, want %q", got, "User")
	}
	// The quota entity is keyed on the NAME alone; getting this wrong writes a quota
	// against a user that does not exist, which the broker accepts without complaint.
	if got := principal.Name(); got != "svc-payments" {
		t.Fatalf("Name() = %q, want %q", got, "svc-payments")
	}

	for _, bad := range []Principal{"", "svc-payments", "User:", ":svc-payments"} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("Principal(%q).Validate() = nil, want a refusal", string(bad))
		}
	}
}

func TestQuotaMustBoundSomething(t *testing.T) {
	for _, quota := range []Quota{{}, {ProducerByteRate: 1}, {ConsumerByteRate: 1}, {ProducerByteRate: -1, ConsumerByteRate: 1}} {
		if err := quota.Validate(); err == nil {
			t.Fatalf("Quota%+v.Validate() = nil, want a refusal — an unbounded principal can starve the cluster", quota)
		}
	}
	if err := (Quota{ProducerByteRate: 1 << 20, ConsumerByteRate: 1 << 20}).Validate(); err != nil {
		t.Fatalf("a positive quota must be accepted, got %v", err)
	}
}
