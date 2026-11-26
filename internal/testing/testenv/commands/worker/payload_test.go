package worker

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/blevinn/aginctus/internal/testing/testenv"
)

func TestPayloadVerifyOffline(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("abc")))
	for _, data := range []string{"abc", "abcd", "ab", "xyz"} {
		var out bytes.Buffer
		cmd := New(Handlers{})
		cmd.SetIn(strings.NewReader(data))
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"payload", "verify", digest, "3"})
		err := cmd.Execute()
		if data == "abc" {
			var r testenv.PayloadReceipt
			if err != nil || json.Unmarshal(out.Bytes(), &r) != nil || r.Status != "integrity_verified" {
				t.Fatalf("unexpected receipt: %s %v", out.String(), err)
			}
		} else if err == nil {
			t.Fatalf("accepted wrong payload")
		}
	}
}
