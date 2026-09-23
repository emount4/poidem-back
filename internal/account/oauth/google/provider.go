// Package google implements the Google OpenID Connect adapter.
package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emount4/poidem-back/internal/account"
)

const (
	defaultAuthorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultTokenEndpoint         = "https://oauth2.googleapis.com/token"
	defaultUserInfoEndpoint      = "https://openidconnect.googleapis.com/v1/userinfo"
	maxResponseBytes             = 1 << 20
)

type Config struct {
	ClientID              string
	ClientSecret          string
	RedirectURL           string
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	HTTPClient            *http.Client
}

type Provider struct {
	clientID, clientSecret, redirectURL                    string
	authorizationEndpoint, tokenEndpoint, userInfoEndpoint string
	client                                                 *http.Client
}

func New(config Config) (*Provider, error) {
	if config.ClientID == "" || config.ClientSecret == "" || config.RedirectURL == "" {
		return nil, errors.New("Google OAuth client ID, secret and redirect URL are required")
	}
	if config.AuthorizationEndpoint == "" {
		config.AuthorizationEndpoint = defaultAuthorizationEndpoint
	}
	if config.TokenEndpoint == "" {
		config.TokenEndpoint = defaultTokenEndpoint
	}
	if config.UserInfoEndpoint == "" {
		config.UserInfoEndpoint = defaultUserInfoEndpoint
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{
		clientID: config.ClientID, clientSecret: config.ClientSecret, redirectURL: config.RedirectURL,
		authorizationEndpoint: config.AuthorizationEndpoint, tokenEndpoint: config.TokenEndpoint,
		userInfoEndpoint: config.UserInfoEndpoint, client: config.HTTPClient,
	}, nil
}

func (p *Provider) AuthorizationURL(state, codeChallenge string) string {
	values := url.Values{
		"client_id":             {p.clientID},
		"redirect_uri":          {p.redirectURL},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return p.authorizationEndpoint + "?" + values.Encode()
}

func (p *Provider) Identity(ctx context.Context, code, codeVerifier string) (account.ExternalIdentity, error) {
	if code == "" || codeVerifier == "" {
		return account.ExternalIdentity{}, errors.New("authorization code and PKCE verifier are required")
	}
	accessToken, err := p.exchange(ctx, code, codeVerifier)
	if err != nil {
		return account.ExternalIdentity{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userInfoEndpoint, nil)
	if err != nil {
		return account.ExternalIdentity{}, fmt.Errorf("create Google userinfo request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := p.client.Do(request)
	if err != nil {
		return account.ExternalIdentity{}, fmt.Errorf("request Google userinfo: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return account.ExternalIdentity{}, fmt.Errorf("Google userinfo returned status %d", response.StatusCode)
	}
	var data struct {
		Subject    string `json:"sub"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
		Picture    string `json:"picture"`
	}
	if err := decodeJSON(response.Body, &data); err != nil {
		return account.ExternalIdentity{}, fmt.Errorf("decode Google userinfo: %w", err)
	}
	if data.Subject == "" {
		return account.ExternalIdentity{}, errors.New("Google userinfo does not contain subject")
	}
	return account.ExternalIdentity{
		Provider: "google", ProviderUserID: data.Subject, FirstName: data.GivenName,
		LastName: data.FamilyName, AvatarURL: data.Picture,
	}, nil
}

func (p *Provider) exchange(ctx context.Context, code, codeVerifier string) (string, error) {
	form := url.Values{
		"client_id": {p.clientID}, "client_secret": {p.clientSecret},
		"code": {code}, "code_verifier": {codeVerifier},
		"grant_type": {"authorization_code"}, "redirect_uri": {p.redirectURL},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create Google token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := p.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("exchange Google authorization code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
		return "", fmt.Errorf("Google token endpoint returned status %d", response.StatusCode)
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := decodeJSON(response.Body, &data); err != nil {
		return "", fmt.Errorf("decode Google token response: %w", err)
	}
	if data.AccessToken == "" {
		return "", errors.New("Google token response does not contain access token")
	}
	return data.AccessToken, nil
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxResponseBytes))
	return decoder.Decode(target)
}
