// version/version_test.go

package version_test

import (
	"testing"

	"github.com/smegg99/goptivum/backend/version"
)

// A node refuses a caller whose version is not major.minor.patch, so an
// unstamped build must not announce the "dev" placeholder.
func TestClientAlwaysAnnouncesARealVersion(t *testing.T) {
	original := version.Release
	t.Cleanup(func() { version.Release = original })

	for _, one := range []struct {
		release, want string
	}{
		{"dev", version.Baseline},
		{"", version.Baseline},
		{"0.2", version.Baseline},
		{"v0.2.0", version.Baseline},
		{"0.2.0-rc1", version.Baseline},
		{"0.2.0", "0.2.0"},
		{"1.10.3", "1.10.3"},
	} {
		version.Release = one.release
		if got := version.Client(); got != one.want {
			t.Errorf("Release %q announced %q, want %q", one.release, got, one.want)
		}
	}
}
