package gateway

import (
	"context"
	"fmt"
	"strings"
)

// CheckFirewalldReloadParity blocks a global reload when the active firewalld
// configuration differs from its persistent counterpart. It is diagnostic,
// never activation approval:
// direct rules, daemon settings, external firewall managers and NixOS ownership
// require independent review.
func CheckFirewalldReloadParity(ctx context.Context, run FirewalldExecutor) error {
	if run == nil {
		return fmt.Errorf("firewalld executor required")
	}
	// --get-default-zone is standalone, not a --permanent query. Daemon
	// settings are deliberately outside this selected-state comparison.
	for _, query := range []string{"--list-all-zones", "--list-all-policies", "--get-ipsets", "--get-services"} {
		runtime, err := run(ctx, query)
		if err != nil {
			return fmt.Errorf("runtime firewalld %s: %w", query, err)
		}
		permanent, err := run(ctx, "--permanent", query)
		if err != nil {
			return fmt.Errorf("permanent firewalld %s: %w", query, err)
		}
		if normalizeFirewalldDisplay(runtime) != normalizeFirewalldDisplay(permanent) {
			return fmt.Errorf("firewalld %s differs between runtime and permanent configuration; global reload unsafe", query)
		}
	}
	return nil
}

// Active/default annotations describe runtime presentation, not permissions.
// Keep every property line intact so bindings and rich-rule drift still fail.
func normalizeFirewalldDisplay(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			for _, suffix := range []string{" (active)", " (default)", " (default, active)"} {
				line = strings.TrimSuffix(line, suffix)
			}
			lines[i] = line
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
