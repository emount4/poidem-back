package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestProviderAuthorizationAndIdentity(t *testing.T) {
	var tokenForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			tokenForm = r.Form
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "provider-access"})
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer provider-access" {
				t.Errorf("Authorization = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{
				"sub": "google-42", "given_name": "Anna", "family_name": "Ivanova", "picture": "https://image.test/avatar",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider, err := New(Config{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://api.test/api/v1/auth/google/callback",
		AuthorizationEndpoint: server.URL + "/authorize", TokenEndpoint: server.URL + "/token",
		UserInfoEndpoint: server.URL + "/userinfo", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizationURL, err := url.Parse(provider.AuthorizationURL("state", "challenge"))
	if err != nil {
		t.Fatal(err)
	}
	query := authorizationURL.Query()
	if query.Get("state") != "state" || query.Get("code_challenge") != "challenge" ||
		query.Get("code_challenge_method") != "S256" || query.Get("scope") != "openid profile email" {
		t.Fatalf("unexpected authorization query: %v", query)
	}

	identity, err := provider.Identity(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Provider != "google" || identity.ProviderUserID != "google-42" || identity.FirstName != "Anna" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
	if tokenForm.Get("code") != "code" || tokenForm.Get("code_verifier") != "verifier" || tokenForm.Get("client_secret") != "secret" {
		t.Fatalf("unexpected token form: %v", tokenForm)
	}
}

func TestProviderRejectsInvalidResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			http.Error(w, "denied", http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	provider, _ := New(Config{
		ClientID: "client", ClientSecret: "secret", RedirectURL: "http://api.test/callback",
		TokenEndpoint: server.URL + "/token", HTTPClient: server.Client(),
	})
	if _, err := provider.Identity(context.Background(), "code", "verifier"); err == nil {
		t.Fatal("expected token endpoint error")
	}
}
