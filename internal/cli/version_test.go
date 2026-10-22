package cli

import (
	"bytes"
	"context"
	"runtime/debug"
	"strings"
	"testing"
)

func TestVersionVCSFallback(t *testing.T) {
	for _, tt := range []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{nil, "unknown"},
		{[]debug.BuildSetting{{Key: "vcs.time", Value: "ignored"}}, "unknown"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "commit"}}, "commit"},
		{[]debug.BuildSetting{{Key: "vcs.revision", Value: "commit"}, {Key: "vcs.modified", Value: "false"}}, "commit"},
		{[]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}, {Key: "vcs.revision", Value: "commit"}}, "commit-dirty"},
		{[]debug.BuildSetting{{Key: "vcs.modified", Value: "true"}}, "unknown"},
	} {
		if got := vcsRevision(tt.settings); got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.settings, got, tt.want)
		}
	}
}

func TestVersionInjectedRevisionOffline(t *testing.T) {
	previous := Revision
	t.Cleanup(func() { Revision = previous })
	for _, revision := range []string{"packaged-commit", "source-sha256-content"} {
		Revision = revision
		for _, args := range [][]string{{"version"}, {"--version"}, {"--configuration-file=/nonexistent", "version"}, {"--configuration-file=/nonexistent", "--version"}} {
			var out, errOut bytes.Buffer
			if code := Run(context.Background(), args, &out, &errOut, nil, nil); code != 0 || errOut.Len() != 0 || strings.TrimSpace(out.String()) != "aginctus "+Version+" (revision "+revision+")" {
				t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, out.String(), errOut.String())
			}
		}
	}
}

func TestVersionEmbeddedSource(t *testing.T) {
    if Version != strings.TrimSpace(embeddedVersion) || Version == "" {
        t.Fatalf("invalid embedded version %q", Version)
    }
    if strings.ContainsAny(Version, " \t\r\n") {
        t.Fatalf("version contains whitespace: %q", Version)
    }
}
