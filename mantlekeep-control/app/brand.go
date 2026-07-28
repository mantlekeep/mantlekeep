package app

import (
	"os"
	"strings"
)

// RemapEnvPrefix lets a white-label wrapper expose its OWN environment namespace
// while the generic core keeps reading MANTLEKEEP_* internally. At startup it copies
// every <from>_X variable to MantleKeep-side <to>_X (only when the target is unset),
// so an operator of a branded binary sets e.g. ACME_SESSION_KEY and never
// sees a MANTLEKEEP_ name.
//
// The core is NEVER renamed — it stays the one shared sovereign engine, read by
// every brand. Only the FACE (the branded binary + its docs) uses the brand
// prefix. This is govern-not-replace applied to configuration: the wrapper
// translates the brand namespace to the engine's; the engine is untouched.
// One call covers every MANTLEKEEP_* var, present and
// future, because it works by prefix, not by an enumerated list.
func RemapEnvPrefix(from, to string) {
	fp, tp := from+"_", to+"_"
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key, val := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(key, fp) {
			continue
		}
		target := tp + strings.TrimPrefix(key, fp)
		if os.Getenv(target) == "" {
			_ = os.Setenv(target, val)
		}
	}
}
