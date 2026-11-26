package testenv

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// StageApprovedPayloadBundle publishes one verified bundle in a private,
// caller-owned directory. This is a local staging primitive, not an archive
// extractor, VM transport, or authority grant. The directory and ancestors
// must be controlled by the trusted operator; same-UID writers are not isolated.
// The caller must bound transport time and aggregate disk use.
func StageApprovedPayloadBundle(input io.Reader, expected, dir string) (PayloadBundleReceipt, error) {
	if input == nil || !vmDigest.MatchString(expected) || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return PayloadBundleReceipt{}, errors.New("invalid payload staging configuration")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return PayloadBundleReceipt{}, errors.New("payload staging requires an existing private directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return PayloadBundleReceipt{}, errors.New("payload staging directory must be caller-owned")
	}
	tmp, err := os.CreateTemp(dir, ".payload-")
	if err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging unavailable")
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	receipt, err := VerifyApprovedPayloadBundle(io.TeeReader(input, tmp), expected)
	if err != nil {
		return PayloadBundleReceipt{}, err
	}
	if err := tmp.Sync(); err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging sync failed")
	}
	if err := tmp.Close(); err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging close failed")
	}
	// Hard-link publication is atomic and never replaces an existing object.
	// Do not adopt existing files, even when their names match the pin.
	name := filepath.Join(dir, expected+".bundle")
	if err := os.Link(tmp.Name(), name); err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging publication failed or name already exists")
	}
	if err := os.Remove(tmp.Name()); err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging temporary cleanup unverified")
	}
	// A sync failure after publication is uncertain; keep the published file
	// for operator inspection rather than deleting a possibly replaced name.
	parent, err := os.Open(dir)
	if err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging publication durability unverified")
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return PayloadBundleReceipt{}, errors.New("payload staging publication durability unverified")
	}
	receipt.Status = "staged_integrity_verified"
	return receipt, nil
}
