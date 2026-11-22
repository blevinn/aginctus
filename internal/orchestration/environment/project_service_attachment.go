package environment

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	incusscope "github.com/blevinn/aginctus/internal/incus"
)

// ProjectServiceAttachment identifies a preprovisioned service instance and
// its external custom volume. The caller MUST independently establish the
// verified server identity and use an authorized ADMINISTRATIVE observation
// connection. This is not a user-facing project authorization interface.
type ProjectServiceAttachment struct {
	Binding Binding
	Instance string
	Pool string
	Volume string
	Device string
	MountPath string
}

// ProjectServiceAttachmentReader has only inspection operations. It cannot
// provision, attach, snapshot, modify, or delete Incus resources.
type ProjectServiceAttachmentReader interface {
	GetConnectionInfo() (*incusclient.ConnectionInfo, error)
	GetProject(string) (*api.Project, string, error)
	GetInstance(string) (*api.Instance, string, error)
	GetStoragePoolVolume(string, string, string) (*api.StorageVolume, string, error)
}

// VerifyProjectServiceAttachment checks one already-created resource pair for
// a dedicated project state service, not for privilege delegation. A user
// supplied Binding can never attest an Incus server's real identity.
func VerifyProjectServiceAttachment(ctx context.Context, reader ProjectServiceAttachmentReader, expected ProjectServiceAttachment) error {
	if ctx == nil { return errors.New("state service inspection requires context") }
	if err := ctx.Err(); err != nil { return err }
	if reader == nil || (Descriptor{ID:"7c82e6d1-1212-4123-8123-0123456789ab", Name:"state-service", Binding:expected.Binding}).Validate() != nil ||
		!validServiceComponent(expected.Instance) || !validServiceComponent(expected.Pool) ||
		!validServiceComponent(expected.Volume) || !validServiceComponent(expected.Device) ||
		!filepath.IsAbs(expected.MountPath) || filepath.Clean(expected.MountPath) != expected.MountPath || expected.MountPath == "/" {
		return errors.New("invalid explicit service volume attachment selection")
	}
	connection, err := reader.GetConnectionInfo()
	if err != nil || connection == nil || connection.Protocol != "incus" ||
		!filepath.IsAbs(connection.SocketPath) || connection.Project != expected.Binding.Project {
		return errors.New("service inspection requires exact administrative project connection")
	}
	if err := ctx.Err(); err != nil { return err }
	scope, err := incusscope.NewProjectScope(expected.Binding.Project)
	if err != nil { return errors.New("invalid selected project scope") }
	project, projectETag, err := reader.GetProject(scope.Name)
	if err != nil || project == nil || projectETag == "" ||
		scope.AssessProject(project.Name, project.Config) != incusscope.ProjectCompatible {
		return errors.New("state-service project ownership or storage isolation unverified")
	}
	instance, etag, err := reader.GetInstance(expected.Instance)
	if err != nil || instance == nil || etag == "" ||
		instance.Project != expected.Binding.Project || instance.Name != expected.Instance ||
		instance.Config["user.aginctus.managed"] != "true" ||
		instance.Config["user.aginctus.resource"] != "project-state-service" {
		return errors.New("service instance identity or ownership unverified")
	}
	device, ok := instance.Devices[expected.Device]
	if !ok || len(device) != 4 || device["type"] != "disk" ||
		device["pool"] != expected.Pool || device["source"] != expected.Volume ||
		device["path"] != expected.MountPath {
		return errors.New("service state-volume device is missing or ambiguous")
	}
	// A second distinct custom volume disk device is a configuration error:
	// the dedicated state service must not silently gain other state volumes.
	for name, other := range instance.Devices {
		if name != expected.Device && other["type"] == "disk" && other["pool"] != "" && other["source"] != "" {
			return errors.New("unexpected additional service storage attachment")
		}
	}
	volume, volumeETag, err := reader.GetStoragePoolVolume(expected.Pool,"custom",expected.Volume)
	if err != nil || volume == nil || volumeETag == "" ||
		volume.Name != expected.Volume || volume.Type != "custom" ||
		volume.ContentType != "filesystem" ||
		volume.Config["user.aginctus.managed"] != "true" ||
		volume.Config["user.aginctus.resource"] != "project-state-volume" {
		return errors.New("project state volume identity or ownership unverified")
	}
	// The Incus UsedBy inventory must prove exclusive attachment. Do not
	// conclude exclusivity from one instance's device map alone.
	if len(volume.UsedBy) != 1 || !instanceUseMatches(volume.UsedBy[0],expected.Instance,expected.Binding.Project) {
		return errors.New("exclusive project state volume attachment unverified")
	}
	if err := ctx.Err(); err != nil { return err }
	second, secondETag, err := reader.GetInstance(expected.Instance)
	if err != nil || second == nil || etag != secondETag ||
		second.Project != instance.Project || second.Name != instance.Name ||
		!equalStringMap(second.Devices[expected.Device],device) {
		return errors.New("service attachment changed during observation")
	}
	again, againETag, err := reader.GetStoragePoolVolume(expected.Pool,"custom",expected.Volume)
	if err != nil || again == nil || volumeETag != againETag ||
		len(again.UsedBy) != 1 || again.UsedBy[0] != volume.UsedBy[0] ||
		again.Name != volume.Name || again.Type != volume.Type ||
		again.ContentType != volume.ContentType || !equalStringMap(again.Config, volume.Config) {
		return errors.New("service volume attachment changed during observation")
	}
	if err := ctx.Err(); err != nil { return err }
	projectAgain, projectETagAgain, err := reader.GetProject(scope.Name)
	if err != nil || projectAgain == nil || projectETagAgain != projectETag ||
		scope.AssessProject(projectAgain.Name, projectAgain.Config) != incusscope.ProjectCompatible {
		return errors.New("state-service project policy changed during observation")
	}
	endConnection, err := reader.GetConnectionInfo()
	if err != nil || endConnection == nil || endConnection.Project != connection.Project ||
		endConnection.Protocol != connection.Protocol || endConnection.SocketPath != connection.SocketPath {
		return errors.New("service project connection changed during observation")
	}
	return ctx.Err()
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

func equalStringMap(a,b map[string]string) bool {
	if len(a)!=len(b) { return false }
	for k,v := range a { if b[k]!=v { return false } }
	return true
}


// ObserveLocalProjectServiceAttachment is an explicit administrator-only,
// read-only helper for the host Incus Unix socket. It must NEVER be called
// from a lower-privilege client, state-service HTTP handler, candidate loader,
// or project-scoped user workflow: the local socket may grant host-wide
// administrator power. It does not create or change Incus resources.
func ObserveLocalProjectServiceAttachment(ctx context.Context, expected ProjectServiceAttachment) error {
	if ctx == nil { return errors.New("state-service attachment inspection requires context") }
	if err := ctx.Err(); err != nil { return err }
	if (Descriptor{ID:"7c82e6d1-1212-4123-8123-0123456789ab", Name:"state-service", Binding:expected.Binding}).Validate()!=nil {
		return errors.New("invalid state-service project selection")
	}
	server, err := incusclient.ConnectIncusUnixWithContext(ctx,"",&incusclient.ConnectionArgs{SkipGetServer:true})
	if err != nil { return errors.New("local administrative Incus observation unavailable") }
	defer server.Disconnect()
	return VerifyProjectServiceAttachment(ctx,server.UseProject(expected.Binding.Project),expected)
}
