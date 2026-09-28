package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/flash"
	"github.com/arashthr/skeleton/internal/platform/logger"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const oauthStateCookie = "oauth_state"

// oauthFlow runs the authorization code flow (with PKCE) for any Provider.
type oauthFlow struct {
	providers *Providers
	service   *Service
	sessions  *Sessions
	secure    bool
}

func newOAuthFlow(cfg *config.Config, providers *Providers, service *Service, sessions *Sessions) *oauthFlow {
	return &oauthFlow{providers: providers, service: service, sessions: sessions, secure: cfg.SecureCookies()}
}

// oauthState is kept in a short-lived cookie between the redirect to the
// provider and the callback.
type oauthState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Next     string `json:"n"`
}

func oauthStartURL(provider, next string) string {
	u := "/oauth/" + provider
	if next != "" {
		u += "?" + url.Values{"next": {next}}.Encode()
	}
	return u
}

func (f *oauthFlow) start(w http.ResponseWriter, r *http.Request) error {
	provider, ok := f.providers.Get(chi.URLParam(r, "provider"))
	if !ok {
		return errs.ErrNotFound
	}

	st := oauthState{State: NewToken(), Verifier: oauth2.GenerateVerifier(), Next: r.FormValue("next")}
	b, _ := json.Marshal(st)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    base64.RawURLEncoding.EncodeToString(b),
		Path:     "/oauth/",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   f.secure,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := provider.Config().AuthCodeURL(st.State, oauth2.S256ChallengeOption(st.Verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
	return nil
}

func (f *oauthFlow) callback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	log := logger.FromContext(ctx)

	provider, ok := f.providers.Get(chi.URLParam(r, "provider"))
	if !ok {
		return errs.ErrNotFound
	}

	st, err := f.readState(w, r)
	if err != nil {
		return err
	}
	if msg := r.FormValue("error"); msg != "" {
		log.Info("oauth denied", zap.String("provider", provider.Name()), zap.String("error", msg))
		return f.fail(w, r, "Sign in with "+provider.Label()+" was cancelled.")
	}

	token, err := provider.Config().Exchange(ctx, r.FormValue("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return fmt.Errorf("%s code exchange: %w", provider.Name(), err)
	}
	ext, err := provider.FetchUser(ctx, token)
	if err != nil {
		return err
	}

	// A signed-in user is connecting another account from the account page.
	if current := user.FromContext(ctx); current != nil {
		if err := f.service.LinkExternal(ctx, current, ext); err != nil {
			if msg, ok := errs.PublicMessage(err); ok {
				flash.Set(w, flash.Error, msg)
				http.Redirect(w, r, "/account", http.StatusSeeOther)
				return nil
			}
			return err
		}
		flash.Set(w, flash.Success, provider.Label()+" account connected.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return nil
	}

	u, err := f.service.SignInExternal(ctx, ext)
	if err != nil {
		if msg, ok := errs.PublicMessage(err); ok && !errors.Is(err, errs.ErrNotFound) {
			return f.fail(w, r, msg)
		}
		return err
	}
	if err := f.sessions.Start(w, r, u); err != nil {
		return err
	}
	http.Redirect(w, r, SafeRedirect(st.Next, "/dashboard"), http.StatusSeeOther)
	return nil
}

// readState validates the state parameter against the cookie and clears it.
func (f *oauthFlow) readState(w http.ResponseWriter, r *http.Request) (oauthState, error) {
	invalid := errs.Public(errs.ErrInvalid, "Sign in session expired, please try again")
	var st oauthState

	c, err := r.Cookie(oauthStateCookie)
	if err != nil {
		return st, invalid
	}
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Path: "/oauth/", MaxAge: -1, HttpOnly: true, Secure: f.secure})

	b, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || json.Unmarshal(b, &st) != nil {
		return st, invalid
	}
	if subtle.ConstantTimeCompare([]byte(st.State), []byte(r.FormValue("state"))) != 1 {
		return st, invalid
	}
	return st, nil
}

func (f *oauthFlow) fail(w http.ResponseWriter, r *http.Request, msg string) error {
	flash.Set(w, flash.Error, msg)
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
	return nil
}
