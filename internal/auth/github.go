package auth

import (
	"context"
	"fmt"
	"strconv"

	"github.com/arashthr/skeleton/internal/platform/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHub struct {
	cfg *oauth2.Config
}

func NewGitHub(client config.OAuthClient, redirectURL string) *GitHub {
	return &GitHub{cfg: &oauth2.Config{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"read:user", "user:email"},
		Endpoint:     github.Endpoint,
	}}
}

func (g *GitHub) Name() string           { return "github" }
func (g *GitHub) Label() string          { return "GitHub" }
func (g *GitHub) Config() *oauth2.Config { return g.cfg }

func (g *GitHub) FetchUser(ctx context.Context, token *oauth2.Token) (ExternalUser, error) {
	client := g.cfg.Client(ctx, token)

	var profile struct {
		ID int64 `json:"id"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user", &profile); err != nil {
		return ExternalUser{}, fmt.Errorf("github user: %w", err)
	}

	// The public profile email may be empty or unverified; ask for the list
	// of addresses and prefer the primary verified one.
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user/emails", &emails); err != nil {
		return ExternalUser{}, fmt.Errorf("github emails: %w", err)
	}

	ext := ExternalUser{Provider: g.Name(), ID: strconv.FormatInt(profile.ID, 10)}
	for _, e := range emails {
		if e.Verified && (e.Primary || ext.Email == "") {
			ext.Email, ext.EmailVerified = e.Email, true
		}
	}
	return ext, nil
}
