package config

import (
	"strings"
	"testing"
	"time"

	estate "github.com/mantlekeep/mantlekeep/mantlekeep-estate"
)

// validDocument is a complete, well-formed config. Tests damage a copy of it, so each case
// changes exactly one thing and the failure it asserts is the thing it changed.
const validDocument = `{
  "floor": {
    "kafka": {
      "dev":    {"producerBytesPerSec": 10485760,  "consumerBytesPerSec": 10485760,  "retention": "168h"},
      "shared": {"producerBytesPerSec": 52428800,  "consumerBytesPerSec": 52428800,  "retention": "720h"},
      "prod":   {"producerBytesPerSec": 104857600, "consumerBytesPerSec": 104857600, "retention": "720h"}
    },
    "postgres": {
      "dev":    {"connectionLimit": 20,  "statementTimeout": "30s", "idleInTransactionTimeout": "1m"},
      "shared": {"connectionLimit": 50,  "statementTimeout": "2m",  "idleInTransactionTimeout": "5m"},
      "prod":   {"connectionLimit": 100, "statementTimeout": "2m",  "idleInTransactionTimeout": "5m"}
    },
    "harbor": {
      "dev":    {"robotExpiry": "2160h"},
      "shared": {"robotExpiry": "2160h"},
      "prod":   {"robotExpiry": "2160h"}
    },
    "app": {
      "enterprise": {
        "dev":    {"replicas": 1, "cpuLimit": "500m", "memoryMiB": 512},
        "shared": {"replicas": 2, "cpuLimit": "1",    "memoryMiB": 1024},
        "prod":   {"replicas": 3, "cpuLimit": "2",    "memoryMiB": 2048}
      },
      "analytics": {
        "dev":    {"replicas": 1, "cpuLimit": "1", "memoryMiB": 2048},
        "shared": {"replicas": 1, "cpuLimit": "2", "memoryMiB": 4096},
        "prod":   {"replicas": 2, "cpuLimit": "2", "memoryMiB": 8192}
      }
    },
    "fleet": {
      "nodePool": {
        "dev":    {"minNodes": 0, "maxNodes": 5,  "allowedInstanceTypes": ["standard-2"]},
        "shared": {"minNodes": 1, "maxNodes": 20, "allowedInstanceTypes": ["standard-4"]},
        "prod":   {"minNodes": 3, "maxNodes": 50, "allowedInstanceTypes": ["standard-8"]}
      },
      "kubernetesVersions": ["1.28", "1.29"]
    }
  },
  "ownership": {}
}`

func TestAValidDocumentLoadsItsLimits(t *testing.T) {
	config, err := Parse([]byte(validDocument))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := config.Floor.Kafka[estate.TierDev].Retention; got != 168*time.Hour {
		t.Errorf("dev kafka retention = %v, want 168h — the authored string must become the "+
			"engine's duration", got)
	}
	if got := config.Floor.Postgres[estate.TierProd].ConnectionLimit; got != 100 {
		t.Errorf("prod postgres connectionLimit = %d, want 100", got)
	}
	if got := config.Floor.App[estate.RuntimeAnalytics][estate.TierProd].MemoryMiB; got != 8192 {
		t.Errorf("prod analytics memoryMiB = %d, want 8192", got)
	}
	if got := config.Floor.Fleet.NodePool[estate.TierDev].MaxNodes; got != 5 {
		t.Errorf("dev nodePool maxNodes = %d, want 5", got)
	}
}

// The floor must come from the file, not from the binary. An absent path is refused rather
// than quietly defaulted, or the deployment governs under limits nobody chose.
func TestAnAbsentConfigPathIsRefused(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Fatal("an empty config path was accepted — the floor would silently come from the binary")
	}
}

// An unknown field is the operator believing they set a limit that was never applied.
func TestAnUnknownFieldIsRefused(t *testing.T) {
	document := strings.Replace(validDocument,
		`"dev":    {"connectionLimit": 20,`,
		`"dev":    {"maxConnections": 20, "connectionLimit": 20,`, 1)
	_, err := Parse([]byte(document))
	if err == nil {
		t.Fatal("an unknown floor field was accepted — a misspelled limit must not be ignored")
	}
	if !strings.Contains(err.Error(), "maxConnections") {
		t.Errorf("the error must name the offending field, got: %v", err)
	}
}

// A bare number would be read as nanoseconds, where "604800" looks like a week and is 0.6ms.
func TestABareNumberIsNotADuration(t *testing.T) {
	document := strings.Replace(validDocument, `"retention": "168h"`, `"retention": 604800`, 1)
	_, err := Parse([]byte(document))
	if err == nil {
		t.Fatal("a bare-number duration was accepted")
	}
	if !strings.Contains(err.Error(), "168h") {
		t.Errorf("the error must show the expected form, got: %v", err)
	}
}

// A tier with no entry is a tier with no limits. Caught at boot, not at the first apply.
func TestAMissingTierIsRefused(t *testing.T) {
	document := strings.Replace(validDocument,
		`"prod":   {"robotExpiry": "2160h"}`, `"uat":    {"robotExpiry": "2160h"}`, 1)
	_, err := Parse([]byte(document))
	if err == nil {
		t.Fatal("a floor missing the prod tier was accepted")
	}
	if !strings.Contains(err.Error(), "harbor") || !strings.Contains(err.Error(), "prod") {
		t.Errorf("the error must name the asset and tier, got: %v", err)
	}
}

// The one that matters: a zero parses cleanly, reads like a limit, and grants an unbounded one.
func TestAZeroLimitIsRefused(t *testing.T) {
	document := strings.Replace(validDocument,
		`"dev":    {"producerBytesPerSec": 10485760,`, `"dev":    {"producerBytesPerSec": 0,`, 1)
	_, err := Parse([]byte(document))
	if err == nil {
		t.Fatal("a zero producer rate was accepted as a floor — that is an unbounded rate " +
			"wearing the name of a quota")
	}
	if !strings.Contains(err.Error(), "producerBytesPerSec") {
		t.Errorf("the error must name the field, got: %v", err)
	}
}

// A credential that never expires is the supply-chain position in an air-gapped footprint.
func TestAZeroRobotExpiryIsRefused(t *testing.T) {
	document := strings.Replace(validDocument, `"dev":    {"robotExpiry": "2160h"}`,
		`"dev":    {"robotExpiry": "0s"}`, 1)
	if _, err := Parse([]byte(document)); err == nil {
		t.Fatal("a robot credential with no expiry was accepted")
	}
}

func TestAnEmptyInstanceTypeListIsRefused(t *testing.T) {
	document := strings.Replace(validDocument, `"allowedInstanceTypes": ["standard-8"]`,
		`"allowedInstanceTypes": []`, 1)
	if _, err := Parse([]byte(document)); err == nil {
		t.Fatal("a node pool with no allowed instance types was accepted — unbounded spend")
	}
}

// Config chooses the policy; it can never reach the guarantee. Naming a governed field under
// "watched" must not demote it, because that would remove a control by adding a word.
func TestConfigCannotDemoteAGovernedFieldToWatched(t *testing.T) {
	document := strings.Replace(validDocument, `"ownership": {}`,
		`"ownership": {"watched": ["digest"]}`, 1)
	config, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !config.Ownership.Owns("digest") {
		t.Fatal("config demoted 'digest' to watched — an unapproved artifact would become a " +
			"line in a report instead of a violation")
	}
	if config.Ownership.Watches("digest") {
		t.Error("'digest' is listed as watched as well as governed; governed must win outright")
	}
}

// Tightening is always allowed: a deployment may take ownership of a field the default watches.
func TestConfigMayPromoteAWatchedFieldToGoverned(t *testing.T) {
	document := strings.Replace(validDocument, `"ownership": {}`,
		`"ownership": {"governed": ["replicas"]}`, 1)
	config, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !config.Ownership.Owns("replicas") {
		t.Fatal("config could not take ownership of 'replicas' — tightening must be permitted")
	}
	if config.Ownership.Watches("replicas") {
		t.Error("'replicas' is still watched after being promoted to governed")
	}
}

// A field the default knows nothing about is added, not dropped: a field in NEITHER map is
// skipped by the differ, which reads exactly like "no drift".
func TestConfigMayAddANewField(t *testing.T) {
	document := strings.Replace(validDocument, `"ownership": {}`,
		`"ownership": {"governed": ["encryptionAtRest"], "watched": ["lastScannedAt"]}`, 1)
	config, err := Parse([]byte(document))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !config.Ownership.Owns("encryptionAtRest") {
		t.Error("a newly governed field was not added")
	}
	if !config.Ownership.Watches("lastScannedAt") {
		t.Error("a newly watched field was not added")
	}
	// The defaults survive the merge.
	if !config.Ownership.Owns("digest") {
		t.Error("merging additions dropped a default governed field")
	}
}
