package testenv

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func bundleFixture(t *testing.T, parts ...string) string {
	t.Helper()
	m := PayloadManifest{
		SchemaVersion: 1, SourceRevision: strings.Repeat("a", 40),
		FlakeLockSHA256: strings.Repeat("b", 64),
	}
	for _, p := range parts {
		m.Segments = append(m.Segments, PayloadSegment{SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(p))), Bytes: int64(len(p))})
	}
	header, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(header) + "\n" + strings.Join(parts, "")
}

func TestVerifyPayloadBundle(t *testing.T) {
	valid := bundleFixture(t, "abc", "defg")
	got, err := VerifyPayloadBundle(strings.NewReader(valid))
	if err != nil || got.Status != "integrity_verified" || got.SourceAuthority != "not_verified" || got.Segments != 2 || got.Bytes != 7 {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	for _, input := range []string{
		valid + "x", valid[:len(valid)-1], strings.Replace(valid, "abcdefg", "defgabc", 1),
		strings.Replace(valid, `"schema_version":1`, `"schema_version":2`, 1),
		strings.Replace(valid, `"schema_version":1`, `"unknown":true,"schema_version":1`, 1),
		strings.Replace(valid, "}\n", "} {}\n", 1),
		strings.Repeat("x", maxPayloadHeaderBytes) + "\n",
		bundleFixture(t),
	} {
		if got, err := VerifyPayloadBundle(strings.NewReader(input)); err == nil || got.Status != "" {
			t.Fatalf("invalid bundle accepted: %+v", got)
		}
	}
}

func TestPayloadBundleAggregateLimitBeforeReadingPayload(t *testing.T) {
	m := PayloadManifest{SchemaVersion: 1, SourceRevision: strings.Repeat("a", 40), FlakeLockSHA256: strings.Repeat("b", 64)}
	for range 9 {
		m.Segments = append(m.Segments, PayloadSegment{SHA256: strings.Repeat("c", 64), Bytes: MaxPayloadSegmentBytes})
	}
	data, _ := json.Marshal(m)
	if _, err := VerifyPayloadBundle(strings.NewReader(string(data) + "\n")); err == nil || !strings.Contains(err.Error(), "metadata bounds") {
		t.Fatalf("aggregate limit not enforced: %v", err)
	}
}
