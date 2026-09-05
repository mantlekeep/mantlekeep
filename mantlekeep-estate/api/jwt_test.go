package api

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// issuer mints tokens the way a real IdP would, so the tests exercise verification rather
// than a stub that agrees with itself.
type issuer struct {
	key   *rsa.PrivateKey
	keyID string
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return &issuer{key: key, keyID: "k1"}
}

func (i *issuer) keySet() *keySetOf { return &keySetOf{id: i.keyID, key: &i.key.PublicKey} }

type keySetOf struct {
	id  string
	key crypto.PublicKey
}

func (k *keySetOf) KeyFor(_ context.Context, keyID string) (crypto.PublicKey, error) {
	if keyID != k.id {
		return nil, errUnknownKey
	}
	return k.key, nil
}

var errUnknownKey = &keyError{}

type keyError struct{}

func (*keyError) Error() string { return "no such key" }

func segment(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// mint builds a signed token. header and claims are taken verbatim so a test can produce a
// malformed one on purpose.
func (i *issuer) mint(t *testing.T, header, claims map[string]any) string {
	t.Helper()
	body := segment(t, header) + "." + segment(t, claims)
	digest := sha256.Sum256([]byte(body))
	signature, err := rsa.SignPKCS1v15(rand.Reader, i.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func goodHeader(i *issuer) map[string]any {
	return map[string]any{"alg": "RS256", "kid": i.keyID, "typ": "JWT"}
}

func goodClaims() map[string]any {
	return map[string]any{
		"iss": "https://idp.example.com",
		"aud": "mantlekeep-estate",
		"sub": "dev-alice",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	}
}

func verifier(i *issuer) *VerifiedCallers {
	return &VerifiedCallers{
		Issuer: "https://idp.example.com", Audience: "mantlekeep-estate", Keys: i.keySet(),
	}
}

func requestWith(token string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/estate/payments", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func TestAValidTokenNamesTheCaller(t *testing.T) {
	idp := newIssuer(t)
	subject, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), goodClaims())))
	if err != nil {
		t.Fatalf("a valid token was refused: %v", err)
	}
	if subject.ID != "dev-alice" {
		t.Errorf("subject = %q, want dev-alice", subject.ID)
	}
}

// THE attack this exists to stop. A port-forward reaches the service directly; without
// verification the caller simply types a header and is believed.
func TestAForgedTokenIsRefused(t *testing.T) {
	real, attacker := newIssuer(t), newIssuer(t)
	attacker.keyID = real.keyID // same kid, different key

	_, err := verifier(real).Caller(requestWith(attacker.mint(t, goodHeader(attacker), goodClaims())))
	if err == nil {
		t.Fatal("a token signed by another key was accepted")
	}
	if !strings.Contains(err.Error(), "signature") {
		t.Errorf("the refusal does not say the signature failed: %v", err)
	}
}

// alg:none is the classic JWT defeat — honouring the algorithm a token names lets the token
// verify itself.
func TestTheNoneAlgorithmIsRefused(t *testing.T) {
	idp := newIssuer(t)
	header := goodHeader(idp)
	header["alg"] = "none"
	body := segment(t, header) + "." + segment(t, goodClaims()) + "."

	if _, err := verifier(idp).Caller(requestWith(body)); err == nil {
		t.Fatal("a token claiming alg:none was accepted")
	}
}

// HS256 with a public key as the HMAC secret is the other classic. The algorithm is checked
// against an allow-list, never used to select one.
func TestASymmetricAlgorithmIsRefused(t *testing.T) {
	idp := newIssuer(t)
	header := goodHeader(idp)
	header["alg"] = "HS256"
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, header, goodClaims()))); err == nil {
		t.Fatal("a token claiming HS256 was accepted")
	}
}

func TestAnExpiredTokenIsRefused(t *testing.T) {
	idp := newIssuer(t)
	claims := goodClaims()
	claims["exp"] = float64(time.Now().Add(-time.Minute).Unix())

	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), claims))); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

// A token with no expiry never stops being valid. Absent must not read as "does not expire".
func TestATokenWithNoExpiryIsRefused(t *testing.T) {
	idp := newIssuer(t)
	claims := goodClaims()
	delete(claims, "exp")
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), claims))); err == nil {
		t.Fatal("a token with no expiry was accepted")
	}
}

// A token minted for another service by the SAME issuer must not be reusable here — that is
// how one compromised service becomes every service.
func TestATokenForAnotherAudienceIsRefused(t *testing.T) {
	idp := newIssuer(t)
	claims := goodClaims()
	claims["aud"] = "some-other-service"
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), claims))); err == nil {
		t.Fatal("a token for another audience was accepted")
	}
}

// aud is a string OR an array in the spec. Handling only one shape means refusing valid
// tokens, which gets the check disabled.
func TestAnArrayAudienceIsAccepted(t *testing.T) {
	idp := newIssuer(t)
	claims := goodClaims()
	claims["aud"] = []any{"another-service", "mantlekeep-estate"}
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), claims))); err != nil {
		t.Fatalf("an array audience containing ours was refused: %v", err)
	}
}

func TestATokenFromAnotherIssuerIsRefused(t *testing.T) {
	idp := newIssuer(t)
	claims := goodClaims()
	claims["iss"] = "https://evil.example.com"
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, goodHeader(idp), claims))); err == nil {
		t.Fatal("a token from another issuer was accepted")
	}
}

// A verifier missing its issuer or audience verifies nothing. It must refuse rather than
// fail open — a broken verifier that accepts everything is worse than the header it replaced.
func TestAnUnconfiguredVerifierRefusesEverything(t *testing.T) {
	idp := newIssuer(t)
	for _, broken := range []*VerifiedCallers{
		{Audience: "mantlekeep-estate", Keys: idp.keySet()},
		{Issuer: "https://idp.example.com", Keys: idp.keySet()},
		{Issuer: "https://idp.example.com", Audience: "mantlekeep-estate"},
	} {
		if _, err := broken.Caller(requestWith(idp.mint(t, goodHeader(idp), goodClaims()))); err == nil {
			t.Error("an unconfigured verifier accepted a token")
		}
	}
}

func TestNoAuthorizationHeaderIsRefused(t *testing.T) {
	idp := newIssuer(t)
	if _, err := verifier(idp).Caller(requestWith("")); err == nil {
		t.Fatal("a request with no bearer token was accepted")
	}
}

// An unknown key id must be an error, not a fallback to trying every key.
func TestAnUnknownKeyIdIsRefused(t *testing.T) {
	idp := newIssuer(t)
	header := goodHeader(idp)
	header["kid"] = "rotated-away"
	if _, err := verifier(idp).Caller(requestWith(idp.mint(t, header, goodClaims()))); err == nil {
		t.Fatal("a token naming an unknown key was accepted")
	}
}
