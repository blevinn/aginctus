package environment

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const projectPromotionPolicyVersion = "aginctus.dev/project-promotion-policy/v1alpha1"

type projectPromotionGrant struct {
	PeerSHA256      string `json:"peerSha256"`
	ServerID        string `json:"serverId"`
	Project         string `json:"project"`
	EnvironmentID   string `json:"environmentId"`
	ExpectedID      string `json:"expectedId"`
	CandidateDigest string `json:"candidateDigest"`
}

// The independently administered guest-local directory and file must not be
// replaceable by the service UID. Reload on every authorization, including the
// CAS publication check, so removing a grant does not require a restart.
func readProjectPromotionPolicy(path string, binding Binding) (map[projectPromotionGrant]struct{}, error) {
	data, err := readAdministratorPolicy(path)
	if err != nil {
		return nil, errLocalPromotionDenied
	}
	return parseProjectPromotionPolicy(data, binding)
}

func readAdministratorPolicy(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errLocalPromotionDenied
	}
	protected := func(info os.FileInfo) bool {
		owner, ok := info.Sys().(*syscall.Stat_t)
		return ok && owner.Uid == 0 && info.Mode().Perm()&0o022 == 0
	}
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || !protected(info) {
			return nil, errLocalPromotionDenied
		}
		if dir == "/" {
			break
		}
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errLocalPromotionDenied
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !protected(info) {
		return nil, errLocalPromotionDenied
	}
	data, err := io.ReadAll(io.LimitReader(f, maxRestrictedOperatorConfigBytes+1))
	if err != nil || len(data) > maxRestrictedOperatorConfigBytes {
		return nil, errLocalPromotionDenied
	}
	return data, nil
}

func parseProjectPromotionPolicy(data []byte, binding Binding) (map[projectPromotionGrant]struct{}, error) {
	var fields map[string]json.RawMessage
	var version string
	var entries []json.RawMessage
	if rejectCandidateDuplicateKeys(data) != nil || json.Unmarshal(data, &fields) != nil ||
		candidateKeys(fields, "apiVersion", "grants") != nil ||
		json.Unmarshal(fields["apiVersion"], &version) != nil || version != projectPromotionPolicyVersion ||
		json.Unmarshal(fields["grants"], &entries) != nil {
		return nil, errLocalPromotionDenied
	}
	grants := make(map[projectPromotionGrant]struct{}, len(entries))
	for _, entry := range entries {
		var members map[string]json.RawMessage
		var g projectPromotionGrant
		if json.Unmarshal(entry, &members) != nil ||
			candidateKeys(members, "peerSha256", "serverId", "project", "environmentId", "expectedId", "candidateDigest") != nil ||
			json.Unmarshal(entry, &g) != nil || !validAcceptedHash(g.PeerSHA256) || !validAcceptedHash(g.CandidateDigest) ||
			(g.ExpectedID != "genesis" && !validAcceptedHash(g.ExpectedID)) ||
			!idPattern.MatchString(g.EnvironmentID) || g.ServerID != binding.ServerID || g.Project != binding.Project {
			return nil, errLocalPromotionDenied
		}
		if _, exists := grants[g]; exists {
			return nil, errLocalPromotionDenied
		}
		grants[g] = struct{}{}
	}
	return grants, nil
}

func mountedProjectPromoter(path string, volume *MountedProjectPublishedSource) func(context.Context, string, LocalPromotionRequest) error {
	return func(ctx context.Context, peer string, req LocalPromotionRequest) error {
		if ctx.Err() != nil || req.Environment.Binding != volume.binding || volume.checkMount(volume.root) != nil {
			return errLocalPromotionDenied
		}
		grants, err := readProjectPromotionPolicy(path, volume.binding)
		if err != nil {
			return errLocalPromotionDenied
		}
		return authorizeProjectPromotion(grants, peer, req)
	}
}

func authorizeProjectPromotion(grants map[projectPromotionGrant]struct{}, peer string, req LocalPromotionRequest) error {
	expected := req.Expected.ID
	if req.Expected == (AcceptedRevision{}) {
		expected = "genesis"
	}
	if _, ok := grants[projectPromotionGrant{
		PeerSHA256: peer, ServerID: req.Environment.Binding.ServerID, Project: req.Environment.Binding.Project,
		EnvironmentID: req.Environment.ID, ExpectedID: expected, CandidateDigest: req.CandidateDigest,
	}]; !ok {
		return errLocalPromotionDenied
	}
	return nil
}
