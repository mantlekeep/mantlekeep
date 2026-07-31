package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"mantlekeep.dev/control/app"
	"mantlekeep.dev/control/doorkit"
	"mantlekeep.dev/control/doorserver"
)

// runServe runs the one door as a service: the mode every SDK client in `service` mode
// talks to, and the shape a multi-service deployment needs — several services, ONE door,
// ONE chain. (Embedded mode gives each process its own chain, which is right for a
// single sovereign zone and wrong when an auditor wants one trail across services.)
//
// Configuration, all optional:
//
//	MANTLEKEEP_ADDR          listen address (default :8080)
//	MANTLEKEEP_AUDIT_PATH    audit chain file (default <data dir>/audit.db)
//	MANTLEKEEP_USER_HEADER   trust this header as the authenticated caller (production)
//	MANTLEKEEP_DEV_LOGIN     "true" enables POST /api/login — DEV ONLY, no credential check
func runServe() {
	address := envOrDefault("MANTLEKEEP_ADDR", ":8080")
	auditPath := envOrDefault("MANTLEKEEP_AUDIT_PATH", filepath.Join(app.DataDir(), "audit.db"))
	userHeader := os.Getenv("MANTLEKEEP_USER_HEADER")
	// Delegation: a service account authenticates as itself and names the person it acts
	// for. Both reach the chain — the person as subject, the service as `via`.
	delegatedHeader := os.Getenv("MANTLEKEEP_ON_BEHALF_OF_HEADER")
	delegators := splitList(os.Getenv("MANTLEKEEP_DELEGATORS"))
	devLogin := os.Getenv("MANTLEKEEP_DEV_LOGIN") == "true"

	// Fail closed: a door with no way to identify callers would deny every request.
	// Say so at startup rather than serving something that cannot work.
	if userHeader == "" && !devLogin {
		log.Fatal("refusing to start: set MANTLEKEEP_USER_HEADER (production) " +
			"or MANTLEKEEP_DEV_LOGIN=true (dev only) so callers can be identified")
	}

	door, err := doorkit.NewInMemoryDoor(auditPath)
	must(err)

	server, err := doorserver.New(doorserver.Options{
		Door:                   door,
		TrustedUserHeader:      userHeader,
		DelegatedSubjectHeader: delegatedHeader,
		Delegators:             delegators,
		DevLogin:               devLogin,
	})
	must(err)

	brand := app.CurrentBrand()
	if brand.Name == "" {
		brand.Name = "MantleKeep"
	}
	fmt.Printf("%s — the one door, listening on %s\n", brand.Name, address)
	fmt.Printf("  chain:    %s\n", auditPath)
	if userHeader != "" {
		fmt.Printf("  identity: trusting header %s\n", userHeader)
	}
	if devLogin {
		fmt.Println("  identity: POST /api/login enabled — DEV ONLY, no credential check")
	}
	if len(delegators) > 0 {
		fmt.Printf("  delegate: %s may act for others via %s\n",
			strings.Join(delegators, ", "), delegatedHeader)
	}
	fmt.Println("  routes:   POST /api/govern · GET /api/audit")

	log.Fatal(http.ListenAndServe(address, server.Handler()))
}

// splitList parses a comma-separated env value, ignoring blanks and stray spaces.
func splitList(value string) []string {
	var items []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
