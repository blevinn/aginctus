package gateway

import (
	"strings"
	"testing"
)

func TestDatabaseRepairPreservesSecretsAndDevices(t *testing.T) {
	inst := ownedInstance()
	inst.Status = "Stopped"
	inst.Config["user.label.incus-compose.service"] = "litellm"
	inst.Config["environment.DATABASE_URL"] = "postgresql://litellm:secret-do-not-echo@postgres:5432/litellm"
	inst.Config["environment.LITELLM_MASTER_KEY"] = "another-secret"
	inst.Devices = map[string]map[string]string{"aginctus_chatgpt_credentials": {"type": "disk", "source": "aginctus-gateway-local-credentials", "pool": "default", "path": credentialMountPath}}
	put, changed, err := reconcileDatabaseInstance(inst, "aginctus-gateway")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if put.Config["environment.DATABASE_URL"] != "postgresql://litellm:secret-do-not-echo@postgres-1:5432/litellm" {
		t.Fatal("incorrect hostname repair")
	}
	if put.Config["environment.LITELLM_MASTER_KEY"] != "another-secret" || put.Devices["aginctus_chatgpt_credentials"]["source"] != "aginctus-gateway-local-credentials" {
		t.Fatal("unrelated configuration changed")
	}
	if inst.Config["environment.DATABASE_URL"] != "postgresql://litellm:secret-do-not-echo@postgres:5432/litellm" {
		t.Fatal("mutated fetched config")
	}
	inst.Config["environment.DATABASE_URL"] = put.Config["environment.DATABASE_URL"]
	_, changed, err = reconcileDatabaseInstance(inst, "aginctus-gateway")
	if err != nil || changed {
		t.Fatalf("not idempotent: %v %v", changed, err)
	}
}

func TestDatabaseRepairFailsClosed(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"foreign host", "postgresql://litellm:secret-do-not-echo@untrusted:5432/litellm"},
		{"no password", "postgresql://litellm@postgres:5432/litellm"},
		{"wrong user", "postgresql://other:secret-do-not-echo@postgres:5432/litellm"},
		{"malformed", "not a URL"},
		{"wrong database", "postgresql://litellm:secret-do-not-echo@postgres:5432/other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := reconcileDatabaseURL(tc.raw)
			if err == nil {
				t.Fatal("accepted invalid URL")
			}
			if strings.Contains(err.Error(), "secret-do-not-echo") {
				t.Fatal("credential leaked in error")
			}
		})
	}
	inst := ownedInstance()
	inst.Status = "Running"
	inst.Config["user.label.incus-compose.service"] = "litellm"
	inst.Config["environment.DATABASE_URL"] = "postgresql://litellm:secret-do-not-echo@postgres:5432/litellm"
	if _, _, err := reconcileDatabaseInstance(inst, "aginctus-gateway"); err == nil {
		t.Fatal("edited running service")
	}
	inst.Status = "Stopped"
	inst.Config["user.aginctus.deployment"] = "foreign"
	if _, _, err := reconcileDatabaseInstance(inst, "aginctus-gateway"); err == nil {
		t.Fatal("edited foreign service")
	}
}
