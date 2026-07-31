package app

import (
	"os"
	"strings"
)

// envPrefix is the engine-side namespace. It appears in this file and NOWHERE in a
// branded product: Brand and CurrentBrand exist precisely so a white-label wrapper
// never has to type — or even know — the framework's variable names.
const envPrefix = "MANTLEKEEP"

// Brand is the ONE call a white-label binary makes to become itself.
//
// It does both halves of the translation, so a product author writes no engine
// variable names at all:
//
//  1. Operator side — every <Prefix>_X in the environment is copied onto the engine's
//     own name, so operators set ACME_POLICY_DIR and never see a MANTLEKEEP_ name.
//  2. Product side — the brand's own defaults are applied WITHOUT the caller spelling
//     out engine variables. An operator-supplied value always wins over a default.
//
// Zero-value fields are left alone, so a wrapper may set only what it wants to change.
//
//	app.Brand(app.BrandOptions{
//	    Prefix: "ACME",
//	    Name:   "Acme Control",
//	    Mark:   "◆",
//	})
//
// The engine is never renamed — only the face is the brand.
func Brand(options BrandOptions) {
	if options.Prefix != "" {
		RemapEnvPrefix(options.Prefix, envPrefix)
	}
	// Applied only where the operator left a gap, so ACME_BRAND_NAME still wins.
	setDefault("BRAND_NAME", options.Name)
	setDefault("BRAND_MARK", options.Mark)
	setDefault("BRAND_KICKER", options.Kicker)
	setDefault("BRAND_TAGLINE", options.Tagline)
}

// BrandOptions is the face a branded binary presents: the operator-facing environment
// prefix plus the display identity. Every field is optional.
type BrandOptions struct {
	Prefix  string // operator-facing env prefix, e.g. "ACME" → operators set ACME_*
	Name    string // display name, e.g. "Acme Control"
	Mark    string // short glyph or symbol
	Kicker  string // short qualifier shown beside the name
	Tagline string // one-line description
}

// CurrentBrand reports the brand in effect after Brand and the environment have been
// resolved, so a product can render its own name without reading engine variables.
func CurrentBrand() BrandOptions {
	return BrandOptions{
		Name:    get("BRAND_NAME"),
		Mark:    get("BRAND_MARK"),
		Kicker:  get("BRAND_KICKER"),
		Tagline: get("BRAND_TAGLINE"),
	}
}

func get(suffix string) string { return os.Getenv(envPrefix + "_" + suffix) }

// setDefault fills an engine variable only when the value is non-empty AND the
// variable is unset — an operator's environment is never overwritten.
func setDefault(suffix, value string) {
	if value == "" {
		return
	}
	key := envPrefix + "_" + suffix
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

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
