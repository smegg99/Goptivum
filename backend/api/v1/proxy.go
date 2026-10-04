// api/v1/proxy.go

package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/smegg99/s99ab/api/apierr"
	"github.com/smegg99/s99ab/net/httpmw"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/backend/core/lockout"
)

// KratosPrefix is where a client reaches Kratos through this server, so a
// school exposes one port instead of two.
const KratosPrefix = "/kratos"

// newKratosProxy forwards /kratos/* to Kratos with the prefix removed.
//
// Kratos serves at its own root and has no route at
// /kratos/self-service/login/api. Forwarding the prefix is a 404 that reads
// like a broken proxy when the whole fault is one missing TrimPrefix.
func newKratosProxy(publicURL string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(strings.TrimRight(publicURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("api: parse kratos public url: %w", err)
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("api: kratos public url needs a scheme and a host, got %q", publicURL)
	}

	return &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetXForwarded()
			request.SetURL(target)
			request.Out.URL.Path = trimKratos(request.Out.URL.Path)
			request.Out.URL.RawPath = trimKratos(request.Out.URL.RawPath)
		},
	}, nil
}

// kratosHandler serves the proxy over a request that always carries a
// cancellation signal, and holds back a sign-in the lockout has blocked.
//
// Without a cancellation signal, ReverseProxy falls back to CloseNotify, and
// gin's writer declares CloseNotifier and then panics when the writer beneath
// it has none. A nil guard counts nothing.
func kratosHandler(proxy *httputil.ReverseProxy, guard *lockout.Guard) gin.HandlerFunc {
	return func(c *gin.Context) {
		counted := guard != nil && isSignIn(c.Request)
		var attempt signIn
		if counted {
			read, err := readSignIn(c)
			switch {
			case errors.Is(err, errSignInTooLarge):
				apierr.Respond(c, apierr.New(apierr.TooLarge, messages.KeyErrorRequestTooLarge))
				return
			case err != nil:
				apierr.Respond(c, apierr.Wrap(err, badRequest, messages.KeyErrorBadRequest))
				return
			}
			if left := guard.Blocked(read.account, read.address); left > 0 {
				refuseBlocked(c, left)
				return
			}
			attempt = read
		}

		request := c.Request
		if request.Context().Done() == nil {
			ctx, cancel := context.WithCancel(request.Context())
			defer cancel()
			request = request.WithContext(ctx)
		}
		proxy.ServeHTTP(c.Writer, request)

		if counted {
			settle(guard, attempt, c.Writer.Status())
		}
	}
}

// trimKratos removes the prefix and never returns an empty path, which is not
// a thing a request line may carry.
func trimKratos(path string) string {
	if path == "" {
		return ""
	}
	trimmed := strings.TrimPrefix(path, KratosPrefix)
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

// registerKratosProxy mounts the proxy on the bare engine.
//
// It is a sibling of the versioned group on purpose: Kratos's own login route
// cannot require the session it exists to issue, and apimount attaches the
// session middleware to the group it returns. That also means apimount's
// limiter never sees these requests, so this group carries its own; an
// unlimited login endpoint on a school LAN is a password guessing machine.
//
// The rate limit slows guessing from one address; the lockout guard stops it
// for a name or an address once too many sign-ins with it failed.
func (s *Server) registerKratosProxy(publicURL string, perMinute int, guard *lockout.Guard) error {
	proxy, err := newKratosProxy(publicURL)
	if err != nil {
		return err
	}
	group := s.engine.Group(KratosPrefix, httpmw.RateLimitWith(httpmw.RateLimitOptions{
		PerMinute: perMinute,
	}))
	group.Any("/*proxyPath", kratosHandler(proxy, guard))
	s.identity = true
	return nil
}
