package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tokenServer is a client-credentials endpoint that issues one token to one client.
func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, secret, ok := r.BasicAuth()
		if r.FormValue("grant_type") != "client_credentials" || !ok || id != "payments-ci" || secret != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"issued-token","token_type":"Bearer"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAPipelineFetchesATokenWithClientCredentials(t *testing.T) {
	t.Setenv(tokenURLEnv, tokenServer(t).URL)
	t.Setenv(clientIDEnv, "payments-ci")
	t.Setenv(clientSecretEnv, "s3cret")
	if token, err := bearerToken(t.Context()); err != nil || token != "issued-token" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

func TestAWrongSecretIsReportedNotSent(t *testing.T) {
	t.Setenv(tokenURLEnv, tokenServer(t).URL)
	t.Setenv(clientIDEnv, "payments-ci")
	t.Setenv(clientSecretEnv, "wrong")
	if _, err := bearerToken(t.Context()); err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("err = %v, want the endpoint's refusal", err)
	}
}

func TestATokenURLWithoutCredentialsIsRefused(t *testing.T) {
	t.Setenv(tokenURLEnv, "https://kc.example/realms/x/protocol/openid-connect/token")
	if _, err := bearerToken(t.Context()); err == nil {
		t.Fatal("a token URL with no client id or secret was accepted")
	}
}

func TestAProvidedTokenWinsAndNothingIsFetched(t *testing.T) {
	t.Setenv(tokenEnv, "given-token")
	t.Setenv(tokenURLEnv, "http://127.0.0.1:1/never-called")
	if token, err := bearerToken(t.Context()); err != nil || token != "given-token" {
		t.Fatalf("token = %q, err = %v", token, err)
	}
}

// Submit presents the token as a Bearer and never the caller header.
func TestSubmitSendsTheBearerNotTheCallerHeader(t *testing.T) {
	var auth, caller string
	estateServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, caller = r.Header.Get("Authorization"), r.Header.Get(estateWebUserHeader)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(estateServer.Close)
	t.Setenv(tokenEnv, "given-token")

	if err := submit(write(t, good), estateServer.URL, "", ""); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if auth != "Bearer given-token" || caller != "" {
		t.Fatalf("Authorization = %q, %s = %q", auth, estateWebUserHeader, caller)
	}
}
