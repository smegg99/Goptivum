// identity/health.go

package identity

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Probe reports whether Kratos answers.
//
// It reads the public health endpoint rather than the admin one: authentication
// is what the API depends on, and the public port is what serves it. A server
// that cannot authenticate anybody is not ready, whatever the database says.
func Probe(publicURL string, timeout time.Duration) (func(context.Context) error, error) {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" {
		return nil, fmt.Errorf("identity: public URL is required")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}

	return func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health/ready", nil)
		if err != nil {
			return fmt.Errorf("identity: build health request: %w", err)
		}
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("identity: kratos is unreachable: %w", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("identity: kratos health answered %d", response.StatusCode)
		}
		return nil
	}, nil
}
