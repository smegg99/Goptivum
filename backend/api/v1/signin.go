// api/v1/signin.go

package v1

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99logger"

	"github.com/smegg99/goptivum/backend/common/logger"
	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/lockout"
)

// maxSignInBody bounds a sign-in body, which is read here before Kratos sees
// it. A real one is well under a kilobyte.
const maxSignInBody = 64 << 10

var errSignInTooLarge = errors.New("api: a sign-in body over 64 KiB")

// signIn is one sign-in submission: the name it signs in with, empty when the
// body carries none, and the client address it comes from.
type signIn struct {
	account string
	address string
}

// isSignIn reports whether a request through the proxy submits a login flow.
// Starting a flow is not a sign-in, so a blocked name can still start one.
func isSignIn(request *http.Request) bool {
	return request.Method == http.MethodPost &&
		path.Clean(trimKratos(request.URL.Path)) == "/self-service/login"
}

// readSignIn reads the name out of the body and puts the body back whole for
// Kratos.
func readSignIn(c *gin.Context) (signIn, error) {
	attempt := signIn{address: c.ClientIP()}
	if c.Request.Body == nil {
		return attempt, nil
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSignInBody+1))
	if err != nil {
		return attempt, err
	}
	if len(raw) > maxSignInBody {
		return attempt, errSignInTooLarge
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	c.Request.ContentLength = int64(len(raw))
	attempt.account = identifierOf(c.ContentType(), raw)
	return attempt, nil
}

// identifierOf is the sign-in name the way Kratos's password method reads it:
// identifier, or the older password_identifier, from JSON or a form.
func identifierOf(contentType string, raw []byte) string {
	if strings.Contains(contentType, "json") {
		var body struct {
			Identifier         string `json:"identifier"`
			PasswordIdentifier string `json:"password_identifier"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return ""
		}
		return firstOf(body.Identifier, body.PasswordIdentifier)
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		return ""
	}
	return firstOf(form.Get("identifier"), form.Get("password_identifier"))
}

func firstOf(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// settle tells the guard how Kratos answered. Kratos answers refused
// credentials with 400 and a disabled account with 401; an expired flow, a
// failing Kratos or anything else is nobody's wrong password and counts
// nothing.
func settle(guard *lockout.Guard, attempt signIn, status int) {
	switch {
	case status >= 200 && status < 300:
		guard.Succeeded(attempt.account)
	case status == http.StatusBadRequest || status == http.StatusUnauthorized:
		block := guard.Failed(attempt.account, attempt.address)
		if !block.Account && !block.Address {
			return
		}
		limit := "name"
		switch {
		case block.Account && block.Address:
			limit = "name and address"
		case block.Address:
			limit = "address"
		}
		// The name stays out of the log: people type passwords into it.
		logger.Log.Warn(s99logger.NewEvent(logger.EventSignInBlocked,
			s99logger.String("address", attempt.address), s99logger.String("limit", limit),
			s99logger.Int("minutes", int(block.For/time.Minute))))
	}
}

// refuseBlocked answers a blocked sign-in with the seconds left. The answer is
// the same for a name no account has, because the guard never asks Kratos.
func refuseBlocked(c *gin.Context, left time.Duration) {
	seconds := strconv.Itoa(int(math.Ceil(left.Seconds())))
	c.Header("Retry-After", seconds)
	apierr.Respond(c, apierr.New(apierr.RateLimited, messages.KeyAuthSignInBlocked).
		WithDetails(map[string]string{"Seconds": seconds}))
}
