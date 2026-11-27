package gateway

import (
	"fmt"
	"strings"
	"testing"
)

func TestGatewayCredentialMaterialRedactsAndValidates(t *testing.T) {
	p, m, s := strings.Repeat("p", 32), strings.Repeat("m", 32), strings.Repeat("s", 32)
	material, err := NewGatewayCredentialMaterial("version-1", p, m, s)
	if err != nil {
		t.Fatal("valid token shape refused")
	}
	for _, pattern := range []string{"%v", "%+v", "%#v"} {
		output := fmt.Sprintf(pattern, material)
		if strings.Contains(output, p) || strings.Contains(output, m) || strings.Contains(output, s) {
			t.Fatal("material disclosed")
		}
	}
	for _, bad := range []string{"short", strings.Repeat(":", 32), strings.Repeat("x", 256), m} {
		if _, err := NewGatewayCredentialMaterial("version-1", bad, m, s); err == nil {
			t.Fatal("invalid material accepted")
		}
	}
}
