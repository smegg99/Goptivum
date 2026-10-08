// arrango/profile.go

package arrango

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	arrangov1 "github.com/smegg99/arrango/backend/gen/arrangov1"
)

// EncodeRules is a rule_config as it is stored beside a job or a result: protobuf JSON.
func EncodeRules(rules *arrangov1.RuleConfig) (string, error) {
	encoded, err := protojson.Marshal(rules)
	if err != nil {
		return "", fmt.Errorf("arrango: encode rules: %w", err)
	}
	return string(encoded), nil
}

// DecodeRules reads what EncodeRules stored.
func DecodeRules(stored string) (*arrangov1.RuleConfig, error) {
	rules := &arrangov1.RuleConfig{}
	if err := protojson.Unmarshal([]byte(stored), rules); err != nil {
		return nil, fmt.Errorf("arrango: decode rules: %w", err)
	}
	return rules, nil
}
