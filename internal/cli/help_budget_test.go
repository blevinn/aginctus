package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestBasicHelpBudgetRealTree checks registered command paths without executing
// their handlers. The full reference remains the authoritative route source;
// no hand-maintained list can become stale as the CLI grows.
func TestBasicHelpBudgetRealTree(t *testing.T) {
	var full, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help", "--full"}, &full, &stderr, nil, nil); code != 0 {
		t.Fatalf("full help failed: code=%d stderr=%s", code, stderr.String())
	}
	paths := map[string]bool{"": true}
	const marker = "Subcommand syntax (derived from registered Cobra commands):"
	inventory := strings.SplitN(full.String(), marker, 2)
	if len(inventory) != 2 { t.Fatal("missing generated command inventory") }
	for _, line := range strings.Split(inventory[1], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "aginctus ") { continue }
		tokens := strings.Fields(strings.TrimPrefix(line, "aginctus "))
		var route []string
		for _, token := range tokens {
			if token == "(" || strings.HasPrefix(token, "[") || token == "--" { break }
			if len(token) == 0 || token[0] < 'a' || token[0] > 'z' { break }
			route = append(route, token)
			paths[strings.Join(route, " ")] = true
		}
	}
	if len(paths) < 15 { t.Fatalf("unexpectedly small command inventory: %d", len(paths)) }

	for path := range paths {
		t.Run(strings.ReplaceAll(path, " ", "/"), func(t *testing.T) {
			args := append(strings.Fields(path), "--help")
			if path == "" { args = []string{"--help"} }
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 0 || errOut.Len() != 0 {
				t.Fatalf("help failed: code=%d stderr=%s", code, errOut.String())
			}
			if strings.Contains(out.String(), "Help layout error:") {
				t.Fatalf("basic help budget exceeded: %s", out.String())
			}
			lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
			if len(lines) > 35 { t.Errorf("help is %d lines (limit 35)", len(lines)) }
			for i, line := range lines {
				if utf8.RuneCountInString(line) > 80 {
					t.Errorf("line %d is %d columns (limit 80): %s", i+1, utf8.RuneCountInString(line), line)
				}
			}
		})
	}
}
