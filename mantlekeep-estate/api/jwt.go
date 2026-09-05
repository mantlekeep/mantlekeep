package api

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	mantlekeep "github.com/mantlekeep/mantlekeep/mantlekeep-control"
)

// VerifiedCallers resolves the caller by VERIFYING a bearer token, rather than trusting a
// header a gateway is supposed to have set.
//
// # Why this exists
//
// [HeaderCallers] is only as good as what sits in front of it. A port-forward, a mesh
// misconfiguration, or a namespace someone can exec into all reach the service directly — and
// then the header is whatever the caller typed. For a read that is a small thing; for an
// approval it is separation of duties defeated by curl.
//
// Verifying here means the estate does not depend on the network path being what it was
// designed to be. A forged token fails on the signature, an expired one on the clock, and a
// token minted for a different service on the audience.
//
// # What it deliberately does not do
//
// It never runs the OIDC login flow — no redirect, no code exchange, no client secret. That
// belongs to the browser and whatever fronts it. This verifies a token somebody else
// obtained, which needs only the issuer's PUBLIC keys and therefore holds no secret at all.
// A control plane holding an OIDC client secret would be a control plane worth stealing.
//
// Roles are not read from the token. The claims say who the caller is and which groups they
// hold; what those mean is the deployment's own mapping, decided server-side. A token that
// could assert its own roles could assert its way past any gate.
type VerifiedCallers struct {
	// Issuer is the `iss` every accepted token must carry.
	Issuer string
	// Audience is the `aud` this service accepts. Required: without it a token minted for
	// ANY service by the same issuer is accepted here, which is how one compromised service
	// becomes every service.
	Audience string
	// Keys supplies the issuer's public keys.
	Keys KeySet
	// Now is the clock, replaceable in a test.
	Now func() time.Time
	// SubjectClaim names the claim carrying the caller's identity. Defaults to "sub";
	// deployments that key their directory on email or preferred_username set it here.
	SubjectClaim string
}

// KeySet supplies an issuer's public keys by key id.
//
// A port because where keys come from is a deployment decision: fetched from a JWKS endpoint
// in a connected estate, mirrored to a file in an air-gapped one. Verification needs only
// public keys, so a mirrored copy is not a secret to protect — which is what makes offline
// verification possible at all.
type KeySet interface {
	// KeyFor returns the public key with this id, or an error.
	//
	// An unknown key id is an error, never a fallback to "try them all": accepting a token
	// signed by a key nobody expected is accepting a token from an issuer nobody approved.
	KeyFor(ctx context.Context, keyID string) (crypto.PublicKey, error)
}

var _ CallerResolver = (*VerifiedCallers)(nil)

// Caller verifies the bearer token and returns the subject it names.
func (v *VerifiedCallers) Caller(request *http.Request) (mantlekeep.Subject, error) {
	if v.Audience == "" || v.Issuer == "" || v.Keys == nil {
		// Refused at the first request rather than silently accepting everything. A verifier
		// missing its issuer or audience verifies nothing, and one that failed open would be
		// worse than the header it replaced.
		return mantlekeep.Subject{}, fmt.Errorf(
			"identity: this verifier is not configured — an issuer, an audience and a key " +
				"source are all required, and a verifier missing any of them verifies nothing")
	}

	raw, err := bearerToken(request)
	if err != nil {
		return mantlekeep.Subject{}, err
	}
	claims, err := v.verify(request.Context(), raw)
	if err != nil {
		return mantlekeep.Subject{}, err
	}

	claim := v.SubjectClaim
	if claim == "" {
		claim = "sub"
	}
	name, _ := claims[claim].(string)
	if strings.TrimSpace(name) == "" {
		return mantlekeep.Subject{}, fmt.Errorf(
			"identity: the token carries no %q claim — a change with nobody behind it cannot be "+
				"governed or attributed", claim)
	}
	// Only the ID travels, exactly as with the header resolver. Roles are the deployment's to
	// resolve from its own directory mapping.
	return mantlekeep.Subject{ID: name}, nil
}

func bearerToken(request *http.Request) (string, error) {
	header := request.Header.Get("Authorization")
	if header == "" {
		return "", fmt.Errorf("identity: no Authorization header — this deployment verifies a " +
			"bearer token rather than trusting a header a proxy was supposed to set")
	}
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", fmt.Errorf("identity: the Authorization header is not a bearer token")
	}
	return strings.TrimSpace(header[len(prefix):]), nil
}

// verify checks the signature, then the claims, and returns them only if both hold.
//
// The ORDER is the guarantee. Claims from an unverified token are attacker-controlled text:
// reading `iss` or `exp` before checking the signature would be trusting the very thing being
// authenticated, and a token can claim any issuer and any expiry it likes.
func (v *VerifiedCallers) verify(ctx context.Context, raw string) (map[string]any, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("identity: the bearer token is not a JWT")
	}

	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return nil, fmt.Errorf("identity: unreadable token header: %w", err)
	}

	// RS256 only, and checked against an allow-list rather than used to select an algorithm.
	// Reading the algorithm FROM the token and honouring it is the classic JWT defeat: "none"
	// verifies everything, and HS256 lets a public key be used as an HMAC secret.
	if header.Algorithm != "RS256" {
		return nil, fmt.Errorf(
			"identity: token algorithm %q is not accepted — this verifier accepts RS256 only, "+
				"because honouring the algorithm a token names is how a token verifies itself",
			header.Algorithm)
	}
	if header.KeyID == "" {
		return nil, fmt.Errorf("identity: the token names no key id")
	}

	key, err := v.Keys.KeyFor(ctx, header.KeyID)
	if err != nil {
		return nil, fmt.Errorf("identity: no key %q: %w", header.KeyID, err)
	}
	publicKey, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("identity: key %q is not an RSA public key", header.KeyID)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("identity: unreadable token signature: %w", err)
	}
	signed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, signed[:], signature); err != nil {
		return nil, fmt.Errorf("identity: the token signature does not verify")
	}

	// Only NOW are the claims worth reading.
	var claims map[string]any
	if err := decodeSegment(parts[1], &claims); err != nil {
		return nil, fmt.Errorf("identity: unreadable token claims: %w", err)
	}
	return claims, v.checkClaims(claims)
}

// checkClaims holds the three things a valid signature does not establish.
func (v *VerifiedCallers) checkClaims(claims map[string]any) error {
	if issuer, _ := claims["iss"].(string); issuer != v.Issuer {
		return fmt.Errorf("identity: token issued by %q, not %q", issuer, v.Issuer)
	}
	// aud is a string OR an array in the spec, and a verifier that handles only one shape
	// accepts tokens it should refuse — or refuses ones it should accept, which gets the
	// check disabled.
	if !audienceMatches(claims["aud"], v.Audience) {
		return fmt.Errorf(
			"identity: this token was not issued for %q — a token minted for another service "+
				"by the same issuer must not be reusable here", v.Audience)
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	expiry, ok := claims["exp"].(float64)
	if !ok {
		// A token with no expiry never stops being valid. Refusing is the only safe reading:
		// treating "absent" as "does not expire" is how a leaked token stays useful forever.
		return fmt.Errorf("identity: the token carries no expiry")
	}
	if now().After(time.Unix(int64(expiry), 0)) {
		return fmt.Errorf("identity: the token expired at %s", time.Unix(int64(expiry), 0).UTC())
	}
	return nil
}

func audienceMatches(claim any, want string) bool {
	switch audience := claim.(type) {
	case string:
		return audience == want
	case []any:
		for _, entry := range audience {
			if text, ok := entry.(string); ok && text == want {
				return true
			}
		}
	}
	return false
}

func decodeSegment(segment string, into any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, into)
}

// StaticKeys is a KeySet over keys already in hand.
//
// The air-gapped case, and the testable one: a zone with no egress mirrors its issuer's JWKS
// to a file and loads it here. Public keys, so the mirror is not a secret — which is what
// makes verifying identity offline possible rather than a compromise.
type StaticKeys struct {
	mu   sync.RWMutex
	keys map[string]crypto.PublicKey
}

// NewStaticKeys builds a key set from a JWKS document.
func NewStaticKeys(jwks []byte) (*StaticKeys, error) {
	var document struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(jwks, &document); err != nil {
		return nil, fmt.Errorf("identity: unreadable JWKS: %w", err)
	}
	set := &StaticKeys{keys: map[string]crypto.PublicKey{}}
	for _, key := range document.Keys {
		if key.Kty != "RSA" || key.Kid == "" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(key.N)
		if err != nil {
			return nil, fmt.Errorf("identity: key %q has an unreadable modulus: %w", key.Kid, err)
		}
		exponent, err := base64.RawURLEncoding.DecodeString(key.E)
		if err != nil {
			return nil, fmt.Errorf("identity: key %q has an unreadable exponent: %w", key.Kid, err)
		}
		set.keys[key.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(modulus),
			E: int(new(big.Int).SetBytes(exponent).Int64()),
		}
	}
	if len(set.keys) == 0 {
		// An empty key set verifies nothing, so every token would fail — which reads as a
		// broken deployment rather than an empty JWKS, and sends an operator hunting the
		// wrong thing.
		return nil, fmt.Errorf("identity: the JWKS contains no usable RSA keys")
	}
	return set, nil
}

// KeyFor implements [KeySet].
func (s *StaticKeys) KeyFor(_ context.Context, keyID string) (crypto.PublicKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key, found := s.keys[keyID]
	if !found {
		return nil, fmt.Errorf("the issuer's key set has no key %q", keyID)
	}
	return key, nil
}
