package gateway

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestOmitFirewalldObjectExactName(t *testing.T) {
	input := "public\n  services: ssh\naginctus-egress\n  services: dns dhcp\naginctus-egress-other\n  services: ftp"
	got := omitFirewalldObject(input, "aginctus-egress")
	if strings.Contains(got, "services: dns dhcp") || !strings.Contains(got, "aginctus-egress-other") || !strings.Contains(got, "services: ssh") {
		t.Fatal(got)
	}
}

func TestCheckFirewalldReloadParity(t *testing.T) {
	for _, drift := range []bool{false, true} {
		var commands []string
		run := func(_ context.Context, args ...string) (string, error) {
			command := strings.Join(args, " ")
			commands = append(commands, command)
			if command == "--permanent --get-default-zone" {
				return "", fmt.Errorf("standalone option does not support --permanent")
			}
			if command == "--list-all-zones" {
				return "public (default, active)\n  services: ssh", nil
			}
			if command == "--permanent --list-all-zones" {
				if drift {
					return "public\n  services: ssh http\naginctus-egress\n  services: dns dhcp", nil
				}
				return "public\n  services: ssh\naginctus-egress\n  services: dns dhcp", nil
			}
			if command == "--list-all-policies" {
				return "aginctus-in (active)\n  target: DROP", nil
			}
			if command == "--permanent --list-all-policies" {
				return "aginctus-in\n  target: DROP\naginctus-egress-https\n  target: CONTINUE", nil
			}
			if strings.HasPrefix(command, "--permanent ") {
				return "same", nil
			}
			if strings.HasPrefix(command, "--") {
				return "same", nil
			}
			return "", fmt.Errorf("unexpected command: %s", command)
		}
		err := CheckFirewalldReloadParity(context.Background(), run)
		if drift && err == nil {
			t.Fatal("accepted runtime/permanent drift")
		}
		if !drift && err != nil {
			t.Fatal(err)
		}
		for _, call := range commands {
			if strings.Contains(call, "--reload") || strings.Contains(call, "--add-") {
				t.Fatalf("mutating operation: %s", call)
			}
		}
	}
}
