package testenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// MaxPayloadSegmentBytes bounds one streamed staging segment, not the total
// run quota. The trusted worker must still enforce aggregate storage budgets.
const MaxPayloadSegmentBytes int64 = 128 * 1024 * 1024

type PayloadReceipt struct {
	SchemaVersion int    `json:"schema_version"`
	SHA256        string `json:"sha256"`
	Bytes         int64  `json:"bytes"`
	Status        string `json:"status"`
}

// VerifyPayloadSegment reads at most expectedBytes+1 bytes, never extracts an
// archive or writes to disk. The expected digest and length must eventually
// come from a separately approved manifest; matching them is not admission,
// provenance, credential scanning, or authorization to install the payload.
func VerifyPayloadSegment(r io.Reader, expectedDigest string, expectedBytes int64) (PayloadReceipt, error) {
	if r == nil || !vmDigest.MatchString(expectedDigest) || expectedBytes <= 0 || expectedBytes > MaxPayloadSegmentBytes {
		return PayloadReceipt{}, errors.New("invalid testenv payload segment metadata")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, expectedBytes+1))
	if err != nil {
		return PayloadReceipt{}, errors.New("testenv payload segment read failed")
	}
	if n != expectedBytes || hex.EncodeToString(h.Sum(nil)) != expectedDigest {
		return PayloadReceipt{}, errors.New("testenv payload segment length or digest mismatch")
	}
	return PayloadReceipt{SchemaVersion: 1, SHA256: expectedDigest, Bytes: n, Status: "integrity_verified"}, nil
}
