// Package account serves the account settings page. The page is assembled
// from sections: account's own (profile, sign-in methods, danger zone) and
// sections contributed by other features (API tokens, billing, ...) through
// the "account_sections" fx group.
package account

import (
	"context"

	"github.com/a-h/templ"
	"github.com/arashthr/skeleton/internal/platform/server"
	"github.com/arashthr/skeleton/internal/user"
	"go.uber.org/fx"
)

var Module = fx.Module("account",
	fx.Provide(
		server.AsRoutes(NewHandler),
		AsSection(newProfileSection),
		AsSection(newSignInSection),
		AsSection(newDangerSection),
	),
)

// Section is one card on the account page.
type Section interface {
	// Order positions the section; lower comes first.
	Order() int
	// Render returns the section for the user, or nil to hide it.
	Render(ctx context.Context, u *user.User) (templ.Component, error)
}

// AsSection annotates a constructor so its result is shown on the account page.
func AsSection(constructor any) any {
	return fx.Annotate(constructor, fx.As(new(Section)), fx.ResultTags(`group:"account_sections"`))
}

// DeletionHook lets features clean up external state (e.g. cancel a Stripe
// subscription) before a user deletes their account. Database rows are
// removed by ON DELETE CASCADE and need no hook.
type DeletionHook interface {
	BeforeUserDelete(ctx context.Context, u *user.User) error
}

// AsDeletionHook annotates a constructor so it runs before account deletion.
func AsDeletionHook(constructor any) any {
	return fx.Annotate(constructor, fx.As(new(DeletionHook)), fx.ResultTags(`group:"account_deletion_hooks"`))
}
