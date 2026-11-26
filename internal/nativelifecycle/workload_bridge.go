package nativelifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/blevinn/aginctus/internal/orchestration/environment"
	"github.com/blevinn/aginctus/internal/orchestration/managementnetwork"
	incus "github.com/lxc/incus/v7/client"
	"golang.org/x/sys/unix"
)

var errWorkloadBridge = errors.New("approved bridge observation unavailable or not converged")

func workloadBridge(g environment.WorkloadHostGrant) managementnetwork.Spec {
	return managementnetwork.Spec{Name: g.Bridge, IPv4Address: g.IPv4Address, IPv4NAT: g.IPv4NAT,
		IPv4Routing: g.IPv4Routing, IPv4DHCPGateway: g.IPv4DHCPGateway, IPv6Address: g.IPv6Address}
}

func bridgeObservation(g environment.WorkloadHostGrant) string {
	data, _ := json.Marshal(g) // The grant contains only primitive fields.
	return fmt.Sprintf("%x\n", sha256.Sum256(data))
}

// ObserveLocalWorkloadBridge serves a single administrator-configured probe.
// It reads no request and never returns Incus metadata or accepts mutation input.
func ObserveLocalWorkloadBridge(ctx context.Context, grantPath string, out io.Writer) error {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return errWorkloadBridge
	}
	grant, err := environment.ReadWorkloadHostGrant(grantPath)
	if err != nil {
		return errWorkloadBridge
	}
	client, err := incus.ConnectIncusUnixWithContext(ctx, "/var/lib/incus/unix.socket", &incus.ConnectionArgs{SkipGetServer: true})
	if err != nil {
		return errWorkloadBridge
	}
	defer client.Disconnect()
	client = client.UseProject("default")
	verifyServer := func() bool {
		s, _, err := client.GetServer()
		return err == nil && s != nil && s.Auth == "trusted" && !s.Environment.ServerClustered &&
			s.Environment.Project == "default" && "sha256:"+s.Environment.CertificateFingerprint == grant.ServerID
	}
	if !verifyServer() || workloadBridge(grant).VerifyNative(ctx, client) != nil || !verifyServer() {
		return errWorkloadBridge
	}
	if current, err := environment.ReadWorkloadHostGrant(grantPath); err != nil || current != grant || ctx.Err() != nil {
		return errWorkloadBridge
	}
	_, err = io.WriteString(out, bridgeObservation(grant))
	return err
}

func verifyWorkloadBridge(ctx context.Context, grant environment.WorkloadHostGrant) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", grant.BridgeSocket)
	if err != nil {
		return errWorkloadBridge
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return errWorkloadBridge
	}
	var root bool
	if err := raw.Control(func(fd uintptr) {
		peer, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		root = err == nil && peer.Uid == 0
	}); err != nil || !root {
		return errWorkloadBridge
	}
	expected := bridgeObservation(grant)
	data, err := io.ReadAll(io.LimitReader(conn, int64(len(expected)+1)))
	if err != nil || string(data) != expected || ctx.Err() != nil {
		return errWorkloadBridge
	}
	return nil
}
