package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/testenv"
)

func TestHelpAndInvalidFlagsNeverContactWorker(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"--help"}, 0}, {[]string{"--exec", "sh"}, 2}, {[]string{"--pool-fingerprint", "--exec"}, 2},
	} {
		var out, err bytes.Buffer
		code := run(context.Background(), tc.args, func(string) string { t.Fatal("read environment"); return "" }, &out, &err,
			func(context.Context, testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error) {
				t.Fatal("contacted daemon")
				return testenv.IncusWorkerPreflightReport{}, nil
			})
		if code != tc.code {
			t.Fatal(code, tc)
		}
	}
}

func TestPreflightErrorRedactionAndInspection(t *testing.T) {
	for _, inspect := range []bool{false, true} {
		var out, stderr bytes.Buffer
		var args []string
		if inspect {
			args = []string{"--pool-fingerprint"}
		}
		code := run(context.Background(), args, func(key string) string { return key }, &out, &stderr,
			func(_ context.Context, cfg testenv.TrustedIncusPreflightOptions) (testenv.IncusWorkerPreflightReport, error) {
				if cfg.InspectPool != inspect || cfg.SocketPath != "AGINCTUS_TESTENV_WORKER_SOCKET" || cfg.PolicyKeyPath != "AGINCTUS_TESTENV_POLICY_KEY_FILE" {
					t.Fatal("incorrect installed configuration", cfg)
				}
				return testenv.IncusWorkerPreflightReport{}, errors.New("private-credential-bearing-daemon-diagnostic")
			})
		if code != 1 || !strings.Contains(out.String(), `"status":"fail"`) || strings.Contains(out.String()+stderr.String(), "private-credential") {
			t.Fatal(code, out.String(), stderr.String())
		}
	}
}
