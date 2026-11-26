package testenv

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const (
	maxPayloadHeaderBytes = 64 * 1024
	maxPayloadSegments    = 32
	MaxPayloadBundleBytes = 1024 * 1024 * 1024
)

var payloadRevision = regexp.MustCompile("^[0-9a-f]{40}$")

type PayloadSegment struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type PayloadManifest struct {
	SchemaVersion   int              `json:"schema_version"`
	SourceRevision  string           `json:"source_revision"`
	SourceDirty     bool             `json:"source_dirty"`
	FlakeLockSHA256 string           `json:"flake_lock_sha256"`
	Segments        []PayloadSegment `json:"segments"`
}

type PayloadBundleReceipt struct {
	SchemaVersion   int    `json:"schema_version"`
	Segments        int    `json:"segments"`
	Bytes           int64  `json:"bytes"`
	Status          string `json:"status"`
	SourceAuthority string `json:"source_authority"`
}

// VerifyPayloadBundle consumes one bounded newline-terminated JSON manifest
// followed by the concatenated ordered raw segments and EOF. No filenames,
// extraction, installation or filesystem writes occur. Manifest claims are
// untrusted and not proof of source identity; independent authorization and
// transport deadlines must precede any future staging.
func VerifyPayloadBundle(input io.Reader) (PayloadBundleReceipt, error) {
	if input == nil {
		return PayloadBundleReceipt{}, errors.New("payload bundle input required")
	}
	r := bufio.NewReaderSize(input, maxPayloadHeaderBytes)
	header, err := r.ReadSlice('\n')
	if err != nil || len(header) >= maxPayloadHeaderBytes {
		return PayloadBundleReceipt{}, errors.New("payload manifest missing or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(header)))
	decoder.DisallowUnknownFields()
	var m PayloadManifest
	if err := decoder.Decode(&m); err != nil {
		return PayloadBundleReceipt{}, errors.New("invalid payload manifest")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return PayloadBundleReceipt{}, errors.New("trailing payload manifest data")
	}
	if m.SchemaVersion != 1 || !payloadRevision.MatchString(m.SourceRevision) ||
		!vmDigest.MatchString(m.FlakeLockSHA256) || len(m.Segments) == 0 || len(m.Segments) > maxPayloadSegments {
		return PayloadBundleReceipt{}, errors.New("invalid payload manifest metadata")
	}
	var total int64
	for _, segment := range m.Segments {
		if !vmDigest.MatchString(segment.SHA256) || segment.Bytes <= 0 || segment.Bytes > MaxPayloadSegmentBytes ||
			segment.Bytes > MaxPayloadBundleBytes-total {
			return PayloadBundleReceipt{}, errors.New("payload bundle exceeds admitted metadata bounds")
		}
		total += segment.Bytes
	}
	for _, segment := range m.Segments {
		if _, err := VerifyPayloadSegment(io.LimitReader(r, segment.Bytes), segment.SHA256, segment.Bytes); err != nil {
			return PayloadBundleReceipt{}, err
		}
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return PayloadBundleReceipt{}, errors.New("payload bundle has trailing bytes or unreadable end")
	}
	return PayloadBundleReceipt{
		SchemaVersion: 1, Segments: len(m.Segments), Bytes: total,
		Status: "integrity_verified", SourceAuthority: "not_verified",
	}, nil
}
