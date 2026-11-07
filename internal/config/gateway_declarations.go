package config

import (
	"fmt"
	"strings"
)

// Protect every configuration source, including dotted overrides, before merge.
// Provider and asset names are identifiers, not credential-bearing field names.
func rejectGatewayCredentialField(path string) error {
	parts := strings.Split(strings.ToLower(path), ".")
	if len(parts) < 4 || parts[0] != "gateway" || parts[1] != "providers" {
		return nil
	}
	for i, field := range parts[3:] {
		index := i + 3
		if index == 5 && len(parts) > 4 && (parts[4] == "models" || parts[4] == "mcps") {
			continue
		}
		if mcpForbiddenFields[field] {
			return fmt.Errorf("gateway provider credential fields cannot be set through configuration")
		}
	}
	return nil
}
