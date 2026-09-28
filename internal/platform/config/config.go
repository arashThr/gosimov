// Package config loads the application configuration from environment
// variables (optionally seeded from a .env file).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"go.uber.org/fx"
)

var Module = fx.Module("config", fx.Provide(Load))

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
)

type Config struct {
	AppName     string      `env:"APP_NAME" envDefault:"Skeleton"`
	Environment Environment `env:"ENVIRONMENT" envDefault:"development"`
	// BaseURL is the public URL of the app, used for links in emails and OAuth callbacks.
	BaseURL  string `env:"BASE_URL" envDefault:"http://localhost:8000"`
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8000"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	Database  Database
	SMTP      SMTP
	Turnstile Turnstile
	Google    OAuthClient `envPrefix:"GOOGLE_"`
	GitHub    OAuthClient `envPrefix:"GITHUB_"`
	Stripe    Stripe
	Admin     Admin
}

type Database struct {
	// Driver is either "sqlite" or "postgres".
	Driver string `env:"DB_DRIVER" envDefault:"sqlite"`
	// URL is a postgres connection string or a SQLite file path.
	URL string `env:"DATABASE_URL" envDefault:"data/app.db"`
}

// SMTP configures outgoing email. When Host is empty, emails are written to the log instead.
type SMTP struct {
	Host     string `env:"SMTP_HOST"`
	Port     int    `env:"SMTP_PORT" envDefault:"587"`
	Username string `env:"SMTP_USER"`
	Password string `env:"SMTP_PASS"`
	From     string `env:"SMTP_FROM" envDefault:"no-reply@example.com"`
}

func (s SMTP) Enabled() bool { return s.Host != "" }

// Turnstile configures Cloudflare Turnstile captcha. Disabled when keys are empty.
type Turnstile struct {
	SiteKey   string `env:"TURNSTILE_SITE_KEY"`
	SecretKey string `env:"TURNSTILE_SECRET_KEY"`
}

func (t Turnstile) Enabled() bool { return t.SiteKey != "" && t.SecretKey != "" }

// OAuthClient holds credentials of an OAuth provider. Disabled when empty.
type OAuthClient struct {
	ClientID     string `env:"CLIENT_ID"`
	ClientSecret string `env:"CLIENT_SECRET"`
}

func (o OAuthClient) Enabled() bool { return o.ClientID != "" && o.ClientSecret != "" }

// Stripe configures subscriptions. Disabled when SecretKey is empty.
type Stripe struct {
	SecretKey     string `env:"STRIPE_SECRET_KEY"`
	PriceID       string `env:"STRIPE_PRICE_ID"`
	WebhookSecret string `env:"STRIPE_WEBHOOK_SECRET"`
}

func (s Stripe) Enabled() bool { return s.SecretKey != "" && s.PriceID != "" }

// Admin configures HTTP basic auth for /admin. Disabled when Password is empty.
type Admin struct {
	User     string `env:"ADMIN_USER" envDefault:"admin"`
	Password string `env:"ADMIN_PASS"`
}

func (a Admin) Enabled() bool { return a.Password != "" }

// Load reads configuration from the environment. Values in a .env file in the
// working directory are loaded first, but never override real env variables.
func Load() (*Config, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	switch c.Environment {
	case EnvDevelopment, EnvProduction:
	default:
		return fmt.Errorf("invalid ENVIRONMENT %q", c.Environment)
	}
	switch c.Database.Driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("invalid DB_DRIVER %q: want sqlite or postgres", c.Database.Driver)
	}
	if _, err := url.ParseRequestURI(c.BaseURL); err != nil {
		return fmt.Errorf("invalid BASE_URL: %w", err)
	}
	return nil
}

func (c *Config) IsProduction() bool { return c.Environment == EnvProduction }

// SecureCookies reports whether cookies should carry the Secure flag.
func (c *Config) SecureCookies() bool { return strings.HasPrefix(c.BaseURL, "https://") }

// URL joins path onto the public base URL.
func (c *Config) URL(path string) string {
	return c.BaseURL + "/" + strings.TrimLeft(path, "/")
}
