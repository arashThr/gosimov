package auth

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/platform/errs"
	"github.com/arashthr/skeleton/internal/platform/flash"
	"github.com/arashthr/skeleton/internal/platform/web"
	"github.com/arashthr/skeleton/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httprate"
)

// Handler serves the sign up, sign in and account recovery pages.
type Handler struct {
	service   *Service
	sessions  *Sessions
	providers *Providers
	turnstile *Turnstile
	oauth     *oauthFlow
}

func NewHandler(service *Service, sessions *Sessions, providers *Providers, turnstile *Turnstile, oauth *oauthFlow) *Handler {
	return &Handler{service: service, sessions: sessions, providers: providers, turnstile: turnstile, oauth: oauth}
}

func (h *Handler) Register(r chi.Router) {
	// Brute-force protection for endpoints that check credentials or send email.
	limit := httprate.Limit(10, time.Minute,
		httprate.WithKeyFuncs(httprate.KeyByIP),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			web.Error(w, r, errs.Public(errs.ErrRateLimited, "Too many attempts, please wait a minute"))
		}))

	r.Group(func(r chi.Router) {
		r.Use(RequireGuest)
		r.Get("/signin", web.Handle(h.signInPage))
		r.Get("/signup", web.Handle(h.signUpPage))
		r.Get("/magic-link", web.Handle(h.magicLinkPage))
		r.Get("/forgot-password", web.Handle(h.forgotPasswordPage))

		r.With(limit).Post("/signin", web.Handle(h.signIn))
		r.With(limit).Post("/signup", web.Handle(h.signUp))
		r.With(limit).Post("/magic-link", web.Handle(h.sendMagicLink))
		r.With(limit).Post("/forgot-password", web.Handle(h.forgotPassword))
	})

	r.Get("/magic-link/verify", web.Handle(h.magicLinkConfirm))
	r.With(limit).Post("/magic-link/verify", web.Handle(h.magicLinkVerify))
	r.Get("/reset-password", web.Handle(h.resetPasswordPage))
	r.With(limit).Post("/reset-password", web.Handle(h.resetPassword))
	r.Get("/verify-email", web.Handle(h.verifyEmail))
	r.With(RequireUser, limit).Post("/verify-email/resend", web.Handle(h.resendVerification))
	r.Post("/signout", web.Handle(h.signOut))

	r.Get("/oauth/{provider}", web.Handle(h.oauth.start))
	r.Get("/oauth/{provider}/callback", web.Handle(h.oauth.callback))
}

func (h *Handler) form(r *http.Request) formData {
	return formData{
		Email:     r.FormValue("email"),
		Next:      r.FormValue("next"),
		Token:     r.FormValue("token"),
		Providers: h.providers.All(),
		Captcha:   h.turnstile.SiteKey,
	}
}

// renderFormError re-renders a form with the error message when the error is
// meant for the user (validation, wrong password, ...), and escalates
// everything else to the central error handler.
func renderFormError(w http.ResponseWriter, r *http.Request, err error, page func(msg string) templ.Component) error {
	msg, ok := errs.PublicMessage(err)
	status := web.StatusFor(err)
	if !ok || status >= 500 {
		return err
	}
	return web.Render(w, r, status, page(msg))
}

func (h *Handler) signInPage(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, SignInPage(h.form(r)))
}

func (h *Handler) signIn(w http.ResponseWriter, r *http.Request) error {
	d := h.form(r)
	fail := func(msg string) templ.Component { d.Error = msg; return SignInPage(d) }

	if err := h.turnstile.Verify(r.Context(), r); err != nil {
		return renderFormError(w, r, err, fail)
	}
	u, err := h.service.SignIn(r.Context(), d.Email, r.FormValue("password"))
	if err != nil {
		return renderFormError(w, r, err, fail)
	}
	if err := h.sessions.Start(w, r, u); err != nil {
		return err
	}
	http.Redirect(w, r, SafeRedirect(d.Next, "/dashboard"), http.StatusSeeOther)
	return nil
}

func (h *Handler) signUpPage(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, SignUpPage(h.form(r)))
}

func (h *Handler) signUp(w http.ResponseWriter, r *http.Request) error {
	d := h.form(r)
	fail := func(msg string) templ.Component { d.Error = msg; return SignUpPage(d) }

	if err := h.turnstile.Verify(r.Context(), r); err != nil {
		return renderFormError(w, r, err, fail)
	}
	u, err := h.service.SignUp(r.Context(), d.Email, r.FormValue("password"))
	if err != nil {
		return renderFormError(w, r, err, fail)
	}
	if err := h.sessions.Start(w, r, u); err != nil {
		return err
	}
	flash.Set(w, flash.Success, "Welcome! We sent you an email to verify your address.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	return nil
}

func (h *Handler) magicLinkPage(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, MagicLinkPage(h.form(r)))
}

func (h *Handler) sendMagicLink(w http.ResponseWriter, r *http.Request) error {
	d := h.form(r)
	fail := func(msg string) templ.Component { d.Error = msg; return MagicLinkPage(d) }

	if err := h.turnstile.Verify(r.Context(), r); err != nil {
		return renderFormError(w, r, err, fail)
	}
	if err := h.service.SendMagicLink(r.Context(), d.Email); err != nil {
		return renderFormError(w, r, err, fail)
	}
	return web.Render(w, r, http.StatusOK, CheckEmailPage(user.NormalizeEmail(d.Email), "We sent a sign-in link to"))
}

func (h *Handler) magicLinkConfirm(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, MagicLinkConfirmPage(r.FormValue("token")))
}

func (h *Handler) magicLinkVerify(w http.ResponseWriter, r *http.Request) error {
	u, err := h.service.ConsumeMagicLink(r.Context(), r.FormValue("token"))
	if err != nil {
		return err
	}
	if err := h.sessions.Start(w, r, u); err != nil {
		return err
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	return nil
}

func (h *Handler) forgotPasswordPage(w http.ResponseWriter, r *http.Request) error {
	return web.Render(w, r, http.StatusOK, ForgotPasswordPage(h.form(r)))
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	d := h.form(r)
	fail := func(msg string) templ.Component { d.Error = msg; return ForgotPasswordPage(d) }

	if err := h.turnstile.Verify(r.Context(), r); err != nil {
		return renderFormError(w, r, err, fail)
	}
	if err := h.service.SendPasswordReset(r.Context(), d.Email); err != nil {
		return renderFormError(w, r, err, fail)
	}
	return web.Render(w, r, http.StatusOK, CheckEmailPage(user.NormalizeEmail(d.Email),
		"If an account exists for this address, we sent a password reset link to"))
}

func (h *Handler) resetPasswordPage(w http.ResponseWriter, r *http.Request) error {
	if r.FormValue("token") == "" {
		return ErrInvalidToken
	}
	return web.Render(w, r, http.StatusOK, ResetPasswordPage(h.form(r)))
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) error {
	d := h.form(r)
	fail := func(msg string) templ.Component { d.Error = msg; return ResetPasswordPage(d) }

	u, err := h.service.ResetPassword(r.Context(), d.Token, r.FormValue("password"))
	if err != nil {
		return renderFormError(w, r, err, fail)
	}
	if err := h.sessions.Start(w, r, u); err != nil {
		return err
	}
	flash.Set(w, flash.Success, "Your password has been updated.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	return nil
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.service.VerifyEmail(r.Context(), r.FormValue("token")); err != nil {
		return err
	}
	flash.Set(w, flash.Success, "Your email address is verified. Thank you!")
	target := "/signin"
	if user.FromContext(r.Context()) != nil {
		target = "/dashboard"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
	return nil
}

// resendVerification is called with htmx from the dashboard banner.
func (h *Handler) resendVerification(w http.ResponseWriter, r *http.Request) error {
	if err := h.service.SendVerificationEmail(r.Context(), user.FromContext(r.Context())); err != nil {
		return err
	}
	if web.IsHTMX(r) {
		_, err := w.Write([]byte("Verification email sent. Check your inbox."))
		return err
	}
	flash.Set(w, flash.Success, "Verification email sent. Check your inbox.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	return nil
}

func (h *Handler) signOut(w http.ResponseWriter, r *http.Request) error {
	if err := h.sessions.End(w, r); err != nil {
		return err
	}
	web.Redirect(w, r, "/")
	return nil
}
