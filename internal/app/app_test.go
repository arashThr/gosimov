package app_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/arashthr/skeleton/internal/app"
	"github.com/arashthr/skeleton/internal/platform/config"
	"github.com/arashthr/skeleton/internal/platform/database/dbtest"
	"github.com/arashthr/skeleton/internal/platform/email"
	"go.uber.org/fx"
)

type outbox struct {
	mu   sync.Mutex
	sent []email.Message
}

func (o *outbox) Send(_ context.Context, m email.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, m)
	return nil
}

// newServer boots the whole application, as main does, against a test
// database and returns a server plus a client that keeps cookies.
func newServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	driver, dsn := dbtest.Config(t)
	cfg := &config.Config{
		AppName:     "Test",
		Environment: config.EnvDevelopment,
		BaseURL:     "http://app.test",
		LogLevel:    "error",
		Database:    config.Database{Driver: driver, URL: dsn},
		Admin:       config.Admin{User: "admin", Password: "secret"},
	}

	var handler http.Handler
	fxApp := fx.New(
		app.Options(),
		fx.Replace(cfg),
		fx.Replace(fx.Annotate(&outbox{}, fx.As(new(email.Sender)))),
		fx.Populate(&handler),
		fx.NopLogger,
	)
	if err := fxApp.Err(); err != nil {
		t.Fatalf("build app: %v", err)
	}

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	return srv, &http.Client{Jar: jar}
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func expect(t *testing.T, resp *http.Response, status int, contains string) string {
	t.Helper()
	b := body(t, resp)
	if resp.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, status)
	}
	if !strings.Contains(b, contains) {
		t.Fatalf("%s %s: body does not contain %q", resp.Request.Method, resp.Request.URL.Path, contains)
	}
	return b
}

func TestUserJourney(t *testing.T) {
	srv, client := newServer(t)
	get := func(path string) *http.Response {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	post := func(path string, form url.Values) *http.Response {
		resp, err := client.PostForm(srv.URL+path, form)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	expect(t, get("/"), http.StatusOK, "Create an account")
	expect(t, get("/healthz"), http.StatusOK, "ok")
	expect(t, get("/does-not-exist"), http.StatusNotFound, "Page not found")

	// Protected pages send anonymous visitors to sign in.
	expect(t, get("/dashboard"), http.StatusOK, "Welcome back")

	// Sign up lands on the dashboard.
	expect(t, post("/signup", url.Values{"email": {"jane@example.com"}, "password": {"password123"}}),
		http.StatusOK, "Signed in as jane@example.com")
	expect(t, get("/account"), http.StatusOK, "API tokens")

	// Create an API token with htmx and use it.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/account/tokens", strings.NewReader("name=cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	token := regexp.MustCompile(`skt_[A-Za-z0-9_-]+`).FindString(expect(t, resp, http.StatusOK, "Copy your new token"))

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me struct{ Email string }
	if err := json.Unmarshal([]byte(expect(t, resp, http.StatusOK, "email")), &me); err != nil || me.Email != "jane@example.com" {
		t.Fatalf("api me: %v %+v", err, me)
	}

	// Sign out, then sign in again.
	expect(t, post("/signout", nil), http.StatusOK, "Create an account")
	expect(t, post("/signin", url.Values{"email": {"jane@example.com"}, "password": {"nope-nope"}}),
		http.StatusUnauthorized, "Email address or password is incorrect")
	expect(t, post("/signin", url.Values{"email": {"jane@example.com"}, "password": {"password123"}, "next": {"/account"}}),
		http.StatusOK, "Sign-in methods")
}

func TestCrossOriginRequestsAreRejected(t *testing.T) {
	srv, client := newServer(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/signin", strings.NewReader("email=a@b.c&password=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, http.StatusForbidden, "Cross-origin request blocked")
}

func TestAdminRequiresBasicAuth(t *testing.T) {
	srv, client := newServer(t)
	resp, err := client.Get(srv.URL + "/admin")
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, http.StatusUnauthorized, "Unauthorized")

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/admin", nil)
	req.SetBasicAuth("admin", "secret")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, resp, http.StatusOK, "registered users")
}
