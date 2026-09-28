package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/errs"
)

var ErrCaptchaFailed = errs.Public(errs.ErrForbidden, "Captcha verification failed, please try again")

// Turnstile verifies Cloudflare Turnstile captcha responses. When no keys
// are configured it is disabled and accepts every request.
type Turnstile struct {
	SiteKey string
	secret  string
	client  *http.Client
}

func NewTurnstile(cfg *config.Config) *Turnstile {
	if !cfg.Turnstile.Enabled() {
		return &Turnstile{}
	}
	return &Turnstile{
		SiteKey: cfg.Turnstile.SiteKey,
		secret:  cfg.Turnstile.SecretKey,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *Turnstile) Enabled() bool { return t.SiteKey != "" }

// Verify checks the "cf-turnstile-response" form value of the request.
func (t *Turnstile) Verify(ctx context.Context, r *http.Request) error {
	if !t.Enabled() {
		return nil
	}
	response := r.FormValue("cf-turnstile-response")
	if response == "" {
		return ErrCaptchaFailed
	}

	form := url.Values{"secret": {t.secret}, "response": {response}, "remoteip": {r.RemoteAddr}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("turnstile request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile siteverify: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("turnstile decode: %w", err)
	}
	if !result.Success {
		return fmt.Errorf("%w (%s)", ErrCaptchaFailed, strings.Join(result.ErrorCodes, ","))
	}
	return nil
}
