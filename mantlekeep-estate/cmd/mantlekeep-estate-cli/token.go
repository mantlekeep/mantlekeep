package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Environment variables a pipeline sets. The secret is read from the environment only — a flag
// value is visible in the process list and in shell history.
//
// #nosec G101 -- these are the NAMES of environment variables, not credentials.
const (
	tokenEnv        = "MANTLEKEEP_TOKEN"              // a token obtained elsewhere
	tokenURLEnv     = "MANTLEKEEP_OIDC_TOKEN_URL"     // or: fetch one with client credentials
	clientIDEnv     = "MANTLEKEEP_OIDC_CLIENT_ID"     //
	clientSecretEnv = "MANTLEKEEP_OIDC_CLIENT_SECRET" //
)

// bearerToken returns the access token to present, or "" when none is configured.
func bearerToken(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(os.Getenv(tokenEnv)); token != "" {
		return token, nil
	}
	tokenURL := strings.TrimSpace(os.Getenv(tokenURLEnv))
	if tokenURL == "" {
		return "", nil
	}
	clientID, secret := os.Getenv(clientIDEnv), os.Getenv(clientSecretEnv)
	if clientID == "" || secret == "" {
		return "", fmt.Errorf("%s is set, so %s and %s are needed too", tokenURLEnv, clientIDEnv, clientSecretEnv)
	}
	if _, err := estateEndpoint(tokenURL); err != nil {
		return "", fmt.Errorf("%s: %w", tokenURLEnv, err)
	}
	return clientCredentials(ctx, tokenURL, clientID, secret)
}

// clientCredentials performs the OAuth 2.0 client-credentials grant (RFC 6749 §4.4).
func clientCredentials(ctx context.Context, tokenURL, clientID, secret string) (string, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	// #nosec G704 -- the token URL is operator-supplied and checked by estateEndpoint.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	// #nosec G704 -- the token URL is operator-supplied and checked by estateEndpoint.
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return "", fmt.Errorf("the token endpoint did not answer: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	var answer struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil || response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the token endpoint refused the client (HTTP %d %s)", response.StatusCode, answer.Error)
	}
	if answer.AccessToken == "" {
		return "", fmt.Errorf("the token endpoint returned no access token")
	}
	return answer.AccessToken, nil
}
