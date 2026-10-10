// api/v1/firstsignin.go

package v1

import (
	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99ab/auth/kratosauth"

	"github.com/smegg99/goptivum/backend/identity"
)

// requirePasswordChanged refuses every route here while an account still
// carries the first-sign-in flag.
//
// Without it the flag is decoration: bootstrap prints a generated password to
// the log once, sets must_change_password, and nothing ever made the account
// act on it, so a password written to a log file kept working for ever. The
// same is true of every account an administrator creates or resets.
//
// The Kratos settings flow is how the password actually changes, and it is
// reached through the /kratos/ proxy, which is registered on the bare engine
// before this group exists. So refusing everything here leaves exactly the one
// route the caller needs to get out of this state.
func requirePasswordChanged(accounts *identity.Admin) gin.HandlerFunc {
	return func(c *gin.Context) {
		who, found := kratosauth.From(c)
		if !found {
			// No session at all is the session middleware's answer to give,
			// not this one's.
			c.Next()
			return
		}
		var must bool
		if err := who.Meta(identity.MetaMustChangePassword, &must); err != nil || !must {
			c.Next()
			return
		}
		if satisfied(c, accounts, who.ID) {
			c.Next()
			return
		}
		// Forbidden and not NotFound: the route is real and this caller may use
		// it, just not before changing the password. A 404 here would send a
		// client hunting for a route that is not missing.
		apierr.Respond(c, apierr.New(apierr.Forbidden, "auth.passwordChangeRequired"))
		c.Abort()
	}
}

// satisfied asks Kratos whether the account has since set its own password, and
// drops the flag when it has.
//
// Kratos owns the settings flow and never touches this server's metadata, so
// the changed password hash is evidence of a change. An
// unreachable admin API answers no: an account that is already refused stays
// refused, rather than an outage becoming a way through.
func satisfied(c *gin.Context, accounts *identity.Admin, id string) bool {
	if accounts == nil {
		return false
	}
	account, err := accounts.Get(c.Request.Context(), id)
	if err != nil {
		return false
	}
	if !account.MustChangePassword {
		// Kratos says the flag is already down, so the session's copy of it is
		// a stale snapshot. Two requests arriving together are enough: the
		// first clears the flag and the second would otherwise find no mark
		// left to compare against and refuse a caller who did what was asked.
		return true
	}
	changed, err := accounts.PasswordChanged(c.Request.Context(), account)
	if err != nil || !changed {
		return false
	}
	cleared, err := accounts.ClearMustChangePassword(c.Request.Context(), id, account.PasswordMark)
	return err == nil && cleared
}
