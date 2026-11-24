package environment

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const ProjectStateReadServiceConfigVersion = "aginctus.dev/project-state-read-service/v1alpha1"

type ProjectStateReadServiceConfig struct {
	APIVersion string `json:"apiVersion"`
	Project string `json:"project"`
	ListenAddress string `json:"listenAddress"`
	MountPath string `json:"mountPath"`
	OperatorConfigFile string `json:"operatorConfigFile"`
	ServiceCertificatePEM string `json:"serviceCertificatePem"`
	ServicePrivateKeyPEM string `json:"servicePrivateKeyPem"`
	TrustedClientCertificatesPEM string `json:"trustedClientCertificatesPem"`
}

func ReadProjectStateReadServiceConfig(path string) (ProjectStateReadServiceConfig, error) {
	deny := func() (ProjectStateReadServiceConfig, error) {
		return ProjectStateReadServiceConfig{}, errors.New("private project state service configuration unavailable")
	}
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path { return deny() }
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil { return deny() }
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maxRestrictedOperatorConfigBytes ||
		info.Mode().Perm()&0o077 != 0 {
		return deny()
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) { return deny() }
	data, err := io.ReadAll(io.LimitReader(file, maxRestrictedOperatorConfigBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxRestrictedOperatorConfigBytes ||
		rejectCandidateDuplicateKeys(data) != nil { return deny() }
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || candidateKeys(fields,
		"apiVersion", "project", "listenAddress", "mountPath", "operatorConfigFile",
		"serviceCertificatePem", "servicePrivateKeyPem", "trustedClientCertificatesPem") != nil {
		return deny()
	}
	var c ProjectStateReadServiceConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		c.APIVersion != ProjectStateReadServiceConfigVersion ||
		c.Project == "" || c.Project == "default" ||
		c.ListenAddress == "" || c.MountPath == "" || c.OperatorConfigFile == "" ||
		c.ServiceCertificatePEM == "" || c.ServicePrivateKeyPEM == "" ||
		c.TrustedClientCertificatesPEM == "" {
		return deny()
	}
	return c, nil
}

func validateProjectStateReadListener(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil { return errors.New("explicit private IP listener required") }
	ip := net.ParseIP(host)
	p, err := strconv.Atoi(port)
	if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) ||
		p < 1 || p > 65535 || err != nil {
		return errors.New("project-state listener must use an explicit private IP and port")
	}
	return nil
}

// ServeMountedProjectStateReads starts a read-only HTTPS service only when
// physical attachment, nonroot service identity, independent restricted Incus
// TLS project verification and *live* peer-certificate trust lookup all pass.
// Incus GetCertificate may be denied under restricted TLS on some versions;
// in that case startup MUST fail rather than use an administrator socket.
//
// The operator/host must independently preflight Incus service instance and
// exclusive project-owned volume attachment before launching this command.
// No provisioning, root delegation, accepted-state writes or reconciliation
// APIs exist on the listener.
func ServeMountedProjectStateReads(ctx context.Context, c ProjectStateReadServiceConfig) error {
	deny := func() error { return errors.New("project state read service preflight failed") }
	if ctx == nil || ctx.Err() != nil || os.Geteuid() == 0 ||
		c.APIVersion != ProjectStateReadServiceConfigVersion ||
		c.Project == "" || c.Project == "default" ||
		validateProjectStateReadListener(c.ListenAddress) != nil {
		return deny()
	}
	operator, err := ReadRestrictedOperatorReadConfig(c.OperatorConfigFile)
	if err != nil || operator.Project != c.Project { return deny() }
	scoped, err := OpenExplicitRestrictedIncusProject(ctx, operator.Connection())
	if err != nil { return deny() }
	defer scoped.Disconnect()
	verifier := RestrictedIncusProjectVerifier{
		Observer: scoped, PinnedServerCertificateSHA256: operator.TrustedServerSHA256,
	}
	volume, err := OpenMountedProjectPublishedSource(ctx, c.MountPath, c.Project, verifier)
	if err != nil { return deny() }
	authorizer := LiveIncusPeerAuthorizer{
		Reader: scoped, Project: c.Project,
		PinnedServerCertificateSHA256: operator.TrustedServerSHA256,
	}
	// A mandatory startup probe rejects inaccessible trust records. This is
	// not sufficient evidence for all callers; each request gets its own
	// fresh peer-specific authorization check.
	operatorKeypair, err := tls.X509KeyPair(
		[]byte(operator.ClientCertificatePEM), []byte(operator.ClientPrivateKeyPEM))
	if err != nil || len(operatorKeypair.Certificate) == 0 { return deny() }
	ownPeer, err := x509.ParseCertificate(operatorKeypair.Certificate[0])
	if err != nil { return deny() }
	if _, err := authorizer.AuthorizeProjectPeer(ctx, ownPeer); err != nil { return deny() }
	serviceKeypair, err := tls.X509KeyPair(
		[]byte(c.ServiceCertificatePEM), []byte(c.ServicePrivateKeyPEM))
	if err != nil { return deny() }
	peerRoots := x509.NewCertPool()
	if !peerRoots.AppendCertsFromPEM([]byte(c.TrustedClientCertificatesPEM)) {
		return deny()
	}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		Certificates: []tls.Certificate{serviceKeypair},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs: peerRoots,
	}
	handler := AuthenticatedAcceptedReadHandler{
		Authorizer: authorizer,
		Catalog: NewRestrictedIncusReadCatalog(scoped, operator.TrustedServerSHA256),
		Source: volume,
	}
	// A TCP listener is permitted only after all authority checks pass.
	listener, err := net.Listen("tcp", c.ListenAddress)
	if err != nil { return errors.New("project state HTTPS listener unavailable") }
	defer listener.Close()
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(tls.NewListener(listener, tlsConfig))
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("project state HTTPS server failed")
	}
	return nil
}
