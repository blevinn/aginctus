package testenv

import (
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestVerifyPayloadSegment(t *testing.T) {
	const data = "test payload"
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
	got, err := VerifyPayloadSegment(strings.NewReader(data), digest, int64(len(data)))
	if err != nil || got.Status != "integrity_verified" || got.Bytes != int64(len(data)) || got.SHA256 != digest {
		t.Fatalf("receipt: %+v %v", got, err)
	}
	for _, input := range []string{"", "short", data + "extra", "Test payload"} {
		if got, err := VerifyPayloadSegment(strings.NewReader(input), digest, int64(len(data))); err == nil || got.Status != "" {
			t.Errorf("unexpected successful receipt: %+v", got)
		}
	}
	r := strings.NewReader(data + "extra")
	if _, err := VerifyPayloadSegment(r, digest, int64(len(data))); err == nil || r.Len() != 4 {
		t.Fatalf("overlong input not bounded: remaining %d, err %v", r.Len(), err)
	}
}

type payloadErrorReader struct{}

func (payloadErrorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestVerifyPayloadSegmentRejectsInvalidMetadataAndIO(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, size := range []int64{-1, 0, MaxPayloadSegmentBytes + 1} {
		if _, err := VerifyPayloadSegment(strings.NewReader("x"), digest, size); err == nil {
			t.Errorf("accepted size %d", size)
		}
	}
	for _, invalid := range []string{"", "sha256:" + digest, strings.ToUpper(digest)} {
		if _, err := VerifyPayloadSegment(strings.NewReader("x"), invalid, 1); err == nil {
			t.Errorf("accepted invalid digest")
		}
	}
	if _, err := VerifyPayloadSegment(payloadErrorReader{}, digest, 1); err == nil {
		t.Fatal("accepted read error")
	}
}
