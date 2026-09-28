package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/arashthr/skeleton/internal/platform/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Google struct {
	cfg *oauth2.Config
}

func NewGoogle(client config.OAuthClient, redirectURL string) *Google {
	return &Google{cfg: &oauth2.Config{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}}
}

func (g *Google) Name() string           { return "google" }
func (g *Google) Label() string          { return "Google" }
func (g *Google) Config() *oauth2.Config { return g.cfg }

func (g *Google) FetchUser(ctx context.Context, token *oauth2.Token) (ExternalUser, error) {
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	client := g.cfg.Client(ctx, token)
	if err := getJSON(ctx, client, "https://openidconnect.googleapis.com/v1/userinfo", &info); err != nil {
		return ExternalUser{}, fmt.Errorf("google userinfo: %w", err)
	}
	return ExternalUser{
		Provider:      g.Name(),
		ID:            info.Sub,
		Email:         info.Email,
		EmailVerified: info.EmailVerified,
	}, nil
}

// getJSON performs a GET request and decodes the JSON response into v.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
