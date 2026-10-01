// identity/rules_test.go

package identity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/smegg99/goptivum/backend/identity"
)

func writeKratosConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kratos.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// What the file leaves out is Kratos v1.3.1's own default, read from its
// embedx/config.schema.json: 8 characters, the similarity check on, the
// breach check on, no breach allowed.
func TestPasswordRulesFallBackToKratosDefaults(t *testing.T) {
	path := writeKratosConfig(t, `
selfservice:
  methods:
    password:
      enabled: true
      config:
        haveibeenpwned_enabled: false
`)
	rules, err := identity.ReadPasswordRules(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := identity.PasswordRules{MinLength: 8, IdentifierSimilarity: true, BreachCheck: false, MaxBreaches: 0}
	if rules != want {
		t.Errorf("rules = %+v, want %+v", rules, want)
	}
}

func TestPasswordRulesReadEveryField(t *testing.T) {
	path := writeKratosConfig(t, `
selfservice:
  methods:
    password:
      config:
        min_password_length: 12
        identifier_similarity_check_enabled: false
        haveibeenpwned_enabled: true
        max_breaches: 3
`)
	rules, err := identity.ReadPasswordRules(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := identity.PasswordRules{MinLength: 12, IdentifierSimilarity: false, BreachCheck: true, MaxBreaches: 3}
	if rules != want {
		t.Errorf("rules = %+v, want %+v", rules, want)
	}
}

// Kratos also reads JSON, which YAML reads as well.
func TestPasswordRulesReadAJSONFile(t *testing.T) {
	path := writeKratosConfig(t, `{"selfservice":{"methods":{"password":{"config":{"min_password_length":10}}}}}`)
	rules, err := identity.ReadPasswordRules(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if rules.MinLength != 10 || !rules.BreachCheck {
		t.Errorf("rules = %+v", rules)
	}
}

func TestPasswordRulesReportAFileTheyCannotUse(t *testing.T) {
	if _, err := identity.ReadPasswordRules(filepath.Join(t.TempDir(), "missing.yml")); err == nil {
		t.Error("a missing file was read")
	}
	if _, err := identity.ReadPasswordRules(writeKratosConfig(t, "selfservice: [not: a map")); err == nil {
		t.Error("a file that does not parse was read")
	}
	if _, err := identity.ReadPasswordRules(writeKratosConfig(t, `
selfservice:
  methods:
    password:
      config:
        min_password_length: eight
`)); err == nil {
		t.Error("a length that is not a number was read")
	}
}

// The file this repository deploys Kratos with, so the rules shown for the
// local server are the ones it runs.
func TestPasswordRulesOfTheDeployedKratos(t *testing.T) {
	rules, err := identity.ReadPasswordRules(filepath.Join("..", "..", "deploy", "kratos", "kratos.yml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := identity.PasswordRules{MinLength: 8, IdentifierSimilarity: true, BreachCheck: false, MaxBreaches: 0}
	if rules != want {
		t.Errorf("rules = %+v, want %+v", rules, want)
	}
}
