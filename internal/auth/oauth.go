package auth

import (
	"context"
	"slices"

	"github.com/arashthr/skeleton/internal/platform/config"
	"golang.org/x/oauth2"
)

// Provider is a third-party identity provider using the OAuth2
// authorization code flow.
//
// To add a provider: implement this interface in a new file (see google.go
// and github.go), add its credentials to config.Config and register it in
// NewProviders.
type Provider interface {
	// Name is the URL-safe identifier, e.g. "google". It is stored in user_identities.
	Name() string
	// Label is shown on the sign-in button, e.g. "Google".
	Label() string
	// Config returns the oauth2 configuration of the provider.
	Config() *oauth2.Config
	// FetchUser returns the signed-in person using an access token.
	FetchUser(ctx context.Context, token *oauth2.Token) (ExternalUser, error)
}

// Providers holds the enabled OAuth providers.
type Providers struct {
	byName map[string]Provider
	list   []Provider
}

// NewProviders registers every provider that has credentials configured.
func NewProviders(cfg *config.Config) *Providers {
	callback := func(name string) string { return cfg.URL("/oauth/" + name + "/callback") }

	var list []Provider
	if cfg.Google.Enabled() {
		list = append(list, NewGoogle(cfg.Google, callback("google")))
	}
	if cfg.GitHub.Enabled() {
		list = append(list, NewGitHub(cfg.GitHub, callback("github")))
	}

	p := &Providers{byName: map[string]Provider{}, list: list}
	for _, provider := range list {
		p.byName[provider.Name()] = provider
	}
	return p
}

func (p *Providers) Get(name string) (Provider, bool) {
	provider, ok := p.byName[name]
	return provider, ok
}

// All returns the enabled providers in display order.
func (p *Providers) All() []Provider { return slices.Clone(p.list) }
