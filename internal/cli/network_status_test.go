package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/config"
)

func TestNetworkStatusLiveStateAndOwnership(t *testing.T) {
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil

	original := inspectIncusNetworks
	t.Cleanup(func() { inspectIncusNetworks = original })

	cases := []struct {
		name   string
		data   string
		err    error
		code   int
		expect string
	}{
		{name: "absent", data: `[]`, code: 0, expect: "absent"},
		{name: "present and owned", data: `[{"name":"aginctus-mgmt","type":"bridge","status":"Created","config":{"user.aginctus.managed":"true","user.aginctus.resource":"management-network"}}]`, code: 0, expect: "present"},
		{name: "foreign network", data: `[{"name":"aginctus-mgmt","type":"bridge","status":"Created","config":{}}]`, code: 1, expect: "not owned"},
		{name: "invalid response", data: `{`, code: 1, expect: "decode"},
		{name: "daemon unavailable", err: errors.New("daemon unavailable"), code: 1, expect: "daemon unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inspectIncusNetworks = func(context.Context) ([]byte, error) {
				return []byte(tc.data), tc.err
			}
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--config=incus.management.network.name=aginctus-mgmt", "network", "status"}, &stdout, &stderr, &fakeIncusClient{}, loader)
			if code != tc.code {
				t.Fatalf("code = %d, want %d; stderr = %q", code, tc.code, stderr.String())
			}
			if !strings.Contains(stdout.String()+stderr.String(), tc.expect) {
				t.Fatalf("output = %q / %q, missing %q", stdout.String(), stderr.String(), tc.expect)
			}
		})
	}
}

func TestNetworkStatusRejectsOptionsWithoutInspecting(t *testing.T) {
	original := inspectIncusNetworks
	t.Cleanup(func() { inspectIncusNetworks = original })
	inspectIncusNetworks = func(context.Context) ([]byte, error) {
		t.Fatal("status with unsupported options must not call Incus")
		return nil, nil
	}
	loader := config.NewLoader()
	loader.SystemPath = ""
	loader.UserPath = ""
	loader.Environment = nil
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"network", "status", "--force"}, &stdout, &stderr, &fakeIncusClient{}, loader)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}
