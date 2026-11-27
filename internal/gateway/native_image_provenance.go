package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/native"
)

const gatewayProvenanceVersion = 1
const gatewayProvenanceLimit = 8192
const gatewayAliasMarkerPrefix = "aginctus-acquired:"

var errGatewayProvenanceAbsent = errors.New("gateway image provenance record absent")
var errGatewayProvenanceUnverified = errors.New("gateway image provenance unavailable or unverified; inspect before retry")

// LocalGatewayImageProvenance is an opt-in same-UID fixture store. The trusted
// composer supplies an existing private directory under trusted ancestors, not
// a path from the candidate. This store does not enforce a write lease, protect
// against its own UID, authenticate a daemon, or authorize reuse or mutation.
type LocalGatewayImageProvenance struct {
	directory string
	protected *environment.ProtectedGatewayGrant
}

func OpenLocalGatewayImageProvenance(directory string) (*LocalGatewayImageProvenance, error) {
	s := &LocalGatewayImageProvenance{directory: directory}
	dir, err := s.openDir()
	if err != nil {
		return nil, err
	}
	return s, dir.Close()
}

func (s *LocalGatewayImageProvenance) openDir() (*os.File, error) {
	if !filepath.IsAbs(s.directory) || filepath.Clean(s.directory) != s.directory {
		return nil, errGatewayProvenanceUnverified
	}
	fd, err := unix.Open(s.directory, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errGatewayProvenanceUnverified
	}
	dir := os.NewFile(uintptr(fd), "gateway provenance directory")
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Uid != uint32(os.Geteuid()) || st.Mode != unix.S_IFDIR|0o700 {
		_ = dir.Close()
		return nil, errGatewayProvenanceUnverified
	}
	return dir, nil
}

type gatewayProvenanceRecord struct {
	Version  int                          `json:"version"`
	Revision environment.AcceptedRevision `json:"revision"`
	Role     string                       `json:"role"`
	Evidence native.GatewayImageEvidence  `json:"evidence"`
}

func gatewayProvenanceName(revision environment.AcceptedRevision, role string) (string, error) {
	if revision.ValidateIdentity() != nil || (role != gatewayRoles[0] && role != gatewayRoles[1]) {
		return "", errGatewayProvenanceUnverified
	}
	return revision.ID + "-" + role + ".json", nil
}

func (s *LocalGatewayImageProvenance) validRecord(r gatewayProvenanceRecord) bool {
	project := r.Revision.Project
	if s.protected != nil {
		g := *s.protected
		if r.Revision.Project != g.Project || r.Revision.ServerID != g.ServerID || r.Revision.EnvironmentID != g.EnvironmentID || r.Revision.ID != g.RevisionID {
			return false
		}
		project = g.ProtectedProject
	}
	return r.Version == gatewayProvenanceVersion && project == r.Evidence.Project &&
		r.Evidence.AliasMarker != "" && r.Evidence.Validate() == nil
}

// ExistingImageEvidence satisfies the binding's existing evidence callback.
// Only exact revision/role records can load; callers still compare the pin and
// independently observe the selected Incus server, image and marked alias.
func (s *LocalGatewayImageProvenance) ExistingImageEvidence(ctx context.Context, revision environment.AcceptedRevision, role string) (native.GatewayImageEvidence, error) {
	empty := native.GatewayImageEvidence{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	name, err := gatewayProvenanceName(revision, role)
	if err != nil {
		return empty, err
	}
	dir, err := s.openDir()
	if err != nil {
		return empty, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return empty, errGatewayProvenanceAbsent
	}
	if err != nil {
		return empty, errGatewayProvenanceUnverified
	}
	file := os.NewFile(uintptr(fd), "gateway provenance record")
	defer file.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil || st.Mode != unix.S_IFREG|0o600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return empty, errGatewayProvenanceUnverified
	}
	data, err := io.ReadAll(io.LimitReader(file, gatewayProvenanceLimit+1))
	var record gatewayProvenanceRecord
	if err != nil || len(data) > gatewayProvenanceLimit || json.Unmarshal(data, &record) != nil ||
		record.Revision != revision || record.Role != role || !s.validRecord(record) {
		return empty, errGatewayProvenanceUnverified
	}
	canonical, err := json.Marshal(record)
	// Enforce our exact wire form (including rejecting duplicate or unknown
	// fields). Reconfirm directory durability even after a prior uncertain sync.
	// This only flushes, never creates or replaces any records.
	if err != nil || !bytes.Equal(data, canonical) || dir.Sync() != nil {
		return empty, errGatewayProvenanceUnverified
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return record.Evidence, nil
}

func newGatewayAliasMarker() string {
	var token [16]byte
	_, _ = rand.Read(token[:]) // Go's crypto/rand.Read fills the buffer or terminates.
	return gatewayAliasMarkerPrefix + hex.EncodeToString(token[:])
}

// record is private: only the binding's fenced, freshly verified acquisition
// and alias effect publishes evidence. Filesystem and Incus are not one
// transaction; incomplete acquisitions are never repaired by inference.
func (s *LocalGatewayImageProvenance) record(ctx context.Context, revision environment.AcceptedRevision, role string, e native.GatewayImageEvidence) error {
	name, err := gatewayProvenanceName(revision, role)
	r := gatewayProvenanceRecord{Version: gatewayProvenanceVersion, Revision: revision, Role: role, Evidence: e}
	if err != nil || !s.validRecord(r) {
		return errGatewayProvenanceUnverified
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > gatewayProvenanceLimit {
		return errGatewayProvenanceUnverified
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := s.openDir()
	if err != nil {
		return err
	}
	defer dir.Close()
	dfd := int(dir.Fd())
	temp := ".pending-" + e.AliasMarker[len(gatewayAliasMarkerPrefix):]
	fd, err := unix.Openat(dfd, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errGatewayProvenanceUnverified
	}
	defer func() { _ = unix.Unlinkat(dfd, temp, 0) }()
	file := os.NewFile(uintptr(fd), "staged gateway provenance record")
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return errGatewayProvenanceUnverified
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if errors.Join(writeErr, syncErr, closeErr) != nil {
		return errGatewayProvenanceUnverified
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if unix.Renameat2(dfd, temp, dfd, name, unix.RENAME_NOREPLACE) != nil || dir.Sync() != nil {
		return errGatewayProvenanceUnverified
	}
	return ctx.Err()
}
