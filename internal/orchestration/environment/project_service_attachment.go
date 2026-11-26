package environment

import (
	"context"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ProjectServiceAttachment identifies a preprovisioned service instance and
// its external custom volume. The caller MUST independently establish the
// verified server identity and use an authorized ADMINISTRATIVE observation
// connection. This is not a user-facing project authorization interface.
type ProjectServiceAttachment struct {
	Binding   Binding
	Instance  string
	Pool      string
	Volume    string
	Device    string
	MountPath string
	// Network is the optional exact managed network for a single eth0 NIC.
	Network string
}

const projectServiceAdminSocket = "/var/lib/incus/unix.socket"

// ProjectServiceAttachmentReader has only inspection operations. It cannot
// provision, attach, snapshot, modify, or delete Incus resources.
type ProjectServiceAttachmentReader interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetServer() (*api.Server, string, error)
	GetProject(string) (*api.Project, string, error)
	GetInstance(string) (*api.Instance, string, error)
	GetStoragePoolVolume(string, string, string) (*api.StorageVolume, string, error)
}

// VerifyProjectServiceAttachment checks one already-created resource pair for
// a dedicated project state service, not for privilege delegation. A user
// supplied Binding can never attest an Incus server's real identity.
func VerifyProjectServiceAttachment(ctx context.Context, reader ProjectServiceAttachmentReader, expected ProjectServiceAttachment) error {
	if ctx == nil {
		return errors.New("state service inspection requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil || !expected.valid() {
		return errors.New("invalid explicit service volume attachment selection")
	}
	connection, err := reader.GetConnectionInfo()
	if err != nil || connection == nil || connection.Protocol != "incus" ||
		connection.SocketPath != projectServiceAdminSocket || connection.Project != expected.Binding.Project {
		return errors.New("service inspection requires exact administrative project connection")
	}
	verifyServer := func() error {
		server, _, err := reader.GetServer()
		if err != nil || server == nil || server.Auth != "trusted" || server.Environment.ServerClustered ||
			server.Environment.Project != expected.Binding.Project || "sha256:"+server.Environment.CertificateFingerprint != expected.Binding.ServerID {
			return errors.New("local Incus server does not match the independent pin")
		}
		return ctx.Err()
	}
	if err := verifyServer(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	scope, err := incusscope.NewProjectScope(expected.Binding.Project)
	if err != nil {
		return errors.New("invalid selected project scope")
	}
	project, projectETag, err := reader.GetProject(scope.Name)
	if err != nil || project == nil || projectETag == "" ||
		project.Config["restricted"] != "true" || scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return errors.New("state-service project ownership or storage isolation unverified")
	}
	instance, etag, err := reader.GetInstance(expected.Instance)
	if err != nil || etag == "" || !expected.confinedInstance(instance) {
		return errors.New("service instance identity, ownership or effective confinement unverified")
	}
	volume, volumeETag, err := reader.GetStoragePoolVolume(expected.Pool, "custom", expected.Volume)
	if err != nil || volume == nil || volumeETag == "" ||
		volume.Name != expected.Volume || volume.Project != expected.Binding.Project || volume.Type != "custom" ||
		volume.ContentType != "filesystem" ||
		volume.Config["user.aginctus.managed"] != "true" ||
		volume.Config["user.aginctus.resource"] != "project-state-volume" {
		return errors.New("project state volume identity or ownership unverified")
	}
	// The Incus UsedBy inventory must prove exclusive attachment. Do not
	// conclude exclusivity from one instance's device map alone.
	if len(volume.UsedBy) != 1 || !instanceUseMatches(volume.UsedBy[0], expected.Instance, expected.Binding.Project) {
		return errors.New("exclusive project state volume attachment unverified")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	second, secondETag, err := reader.GetInstance(expected.Instance)
	if err != nil || etag != secondETag || !expected.confinedInstance(second) ||
		!second.CreatedAt.Equal(instance.CreatedAt) || !maps.Equal(second.ExpandedConfig, instance.ExpandedConfig) ||
		!maps.EqualFunc(second.ExpandedDevices, instance.ExpandedDevices, maps.Equal) {
		return errors.New("service attachment changed during observation")
	}
	again, againETag, err := reader.GetStoragePoolVolume(expected.Pool, "custom", expected.Volume)
	if err != nil || again == nil || volumeETag != againETag ||
		len(again.UsedBy) != 1 || again.UsedBy[0] != volume.UsedBy[0] ||
		again.Name != volume.Name || again.Project != volume.Project || again.Type != volume.Type ||
		again.ContentType != volume.ContentType || !equalStringMap(again.Config, volume.Config) {
		return errors.New("service volume attachment changed during observation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	projectAgain, projectETagAgain, err := reader.GetProject(scope.Name)
	if err != nil || projectAgain == nil || projectETagAgain != projectETag ||
		projectAgain.Config["restricted"] != "true" || scope.AssessProject(projectAgain.Name, projectAgain.Config) != incusscope.ProjectCompatible {
		return errors.New("state-service project policy changed during observation")
	}
	endConnection, err := reader.GetConnectionInfo()
	if err != nil || endConnection == nil || endConnection.Project != connection.Project ||
		endConnection.Protocol != connection.Protocol || endConnection.SocketPath != connection.SocketPath {
		return errors.New("service project connection changed during observation")
	}
	return verifyServer()
}

func (a ProjectServiceAttachment) valid() bool {
	pin, pinned := strings.CutPrefix(a.Binding.ServerID, "sha256:")
	_, scopeErr := incusscope.NewProjectScope(a.Binding.Project)
	return pinned && validAcceptedHash(pin) && scopeErr == nil &&
		validServiceComponent(a.Instance) && validServiceComponent(a.Pool) &&
		validServiceComponent(a.Volume) && validServiceComponent(a.Device) &&
		(a.Network == "" || validServiceComponent(a.Network)) &&
		filepath.IsAbs(a.MountPath) && filepath.Clean(a.MountPath) == a.MountPath && a.MountPath != "/" &&
		!strings.ContainsAny(a.MountPath, "\x00 \t\r\n\\")
}

func (a ProjectServiceAttachment) confinedInstance(i *api.Instance) bool {
	if i == nil || i.Project != a.Binding.Project || i.Name != a.Instance || i.Type != "container" || i.ExpandedConfig == nil ||
		i.Config["user.aginctus.managed"] != "true" || i.Config["user.aginctus.resource"] != "project-state-service" {
		return false
	}
	for key, value := range i.Config {
		if expanded, found := i.ExpandedConfig[key]; !found || expanded != value {
			return false
		}
	}
	for key, value := range i.ExpandedConfig {
		if strings.HasPrefix(key, "raw.") || key == "linux.kernel_modules" ||
			(strings.HasPrefix(key, "security.") && !(key == "security.privileged" && value == "false")) {
			return false
		}
	}
	wantDisk := map[string]string{"type": "disk", "pool": a.Pool, "source": a.Volume, "path": a.MountPath}
	if !maps.Equal(i.Devices[a.Device], wantDisk) || !maps.Equal(i.ExpandedDevices[a.Device], wantDisk) {
		return false
	}
	for name, device := range i.Devices {
		if !maps.Equal(device, i.ExpandedDevices[name]) {
			return false
		}
	}
	root, nic := false, false
	for name, dev := range i.ExpandedDevices {
		if name == a.Device {
			continue
		}
		switch {
		case !root && len(dev) == 3 && dev["type"] == "disk" && dev["path"] == "/" && validServiceComponent(dev["pool"]):
			root = true
		case !nic && a.Network != "" && maps.Equal(dev, map[string]string{"type": "nic", "network": a.Network, "name": "eth0"}):
			nic = true
		default:
			return false
		}
	}
	return root && nic == (a.Network != "")
}

func validServiceComponent(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func instanceUseMatches(raw, name, project string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Fragment != "" || u.User != nil ||
		u.Path != "/1.0/instances/"+name {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && len(q) == 1 && len(q["project"]) == 1 && q.Get("project") == project
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ObserveLocalProjectServiceAttachment is an explicit administrator-only,
// read-only helper for the host Incus Unix socket. It must NEVER be called
// from a lower-privilege client, state-service HTTP handler, candidate loader,
// or project-scoped user workflow: the local socket may grant host-wide
// administrator power. It does not create or change Incus resources.
func ObserveLocalProjectServiceAttachment(ctx context.Context, expected ProjectServiceAttachment) error {
	if ctx == nil {
		return errors.New("state-service attachment inspection requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return errors.New("state service preflight requires direct local administrator invocation")
	}
	if !expected.valid() {
		return errors.New("invalid state-service project selection")
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx, projectServiceAdminSocket, &incusclient.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return errors.New("local administrative Incus observation unavailable")
	}
	defer server.Disconnect()
	return VerifyProjectServiceAttachment(ctx, server.UseProject(expected.Binding.Project), expected)
}
