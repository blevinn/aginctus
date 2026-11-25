package nativeoci_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	incus "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"

	"github.com/blevinn/aginctus/internal/orchestration/nativeoci"
)

const (
	postgresFixturePasswordPath  = "/var/lib/postgresql/.aginctus-bootstrap"
	postgresFixturePassPath      = "/var/lib/postgresql/.aginctus-pgpass"
	postgresFixtureWrongPassPath = "/var/lib/postgresql/.aginctus-wrong-pgpass"
	// Docker Official Images source ac9627dcf544a8454f212a12ddeebfcdc0803d3e,
	// 17/alpine3.24/docker-entrypoint.sh. Check bytes in the manifest-pinned
	// converted guest before delivering even a disposable credential.
	postgresFixtureEntrypointSHA256 = "48647812e68165e2943abe82a1a7cb8364d6e9f8b6b6aefc179868b66a327c46"
)

// Only reachable from the explicitly disposable, local-daemon OCI spike. The
// Incus admin file API can read these files: this is consumption evidence,
// not evidence for an approved secret publisher or restricted principal.
type postgresFileFixture struct{ accepted, wrong string }

func newPostgresFileFixture(t *testing.T, config nativeoci.ImageConfig) postgresFileFixture {
	t.Helper()
	if !slices.Equal(config.Entrypoint, []string{"docker-entrypoint.sh"}) || !slices.Equal(config.Cmd, []string{"postgres"}) ||
		!slices.Contains(config.Env, "PG_MAJOR=17") || !slices.Contains(config.Env, "PGDATA=/var/lib/postgresql/data") || config.User != "" {
		t.Fatal("unexpected PostgreSQL 17 Alpine OCI bootstrap contract")
	}
	makePassword := func() string {
		var data [24]byte
		if _, err := rand.Read(data[:]); err != nil {
			t.Fatal("synthetic credential generation failed")
		}
		return hex.EncodeToString(data[:])
	}
	return postgresFileFixture{accepted: makePassword(), wrong: makePassword()}
}

func (f postgresFileFixture) stage(t *testing.T, sdk incus.InstanceServer, name string, retained bool) {
	t.Helper()
	guest, _, err := sdk.GetInstance(name)
	if err != nil || guest == nil || guest.StatusCode != api.Stopped {
		t.Fatal("password file target must be stopped")
	}
	script, _, err := sdk.GetInstanceFile(name, "/usr/local/bin/docker-entrypoint.sh")
	if err != nil || script == nil {
		t.Fatal("missing PostgreSQL OCI entrypoint")
	}
	defer script.Close()
	h := sha256.New()
	const limit = 32 << 10
	n, err := io.Copy(h, io.LimitReader(script, limit+1))
	if err != nil || n > limit || hex.EncodeToString(h.Sum(nil)) != postgresFixtureEntrypointSHA256 {
		t.Fatal("PostgreSQL OCI entrypoint differs from reviewed upstream bytes")
	}
	bootstrap := f.accepted
	if retained {
		bootstrap = f.wrong
	}
	for _, entry := range []struct{ path, data string }{
		{postgresFixturePasswordPath, bootstrap + "\n"},
		{postgresFixturePassPath, "127.0.0.1:5432:litellm:litellm:" + f.accepted + "\n"},
		{postgresFixtureWrongPassPath, "127.0.0.1:5432:litellm:litellm:" + f.wrong + "\n"},
	} {
		// Refuse a pre-existing image path. The disposable daemon is sole
		// writer, with a verified OCI entrypoint and a stopped target.
		prior, _, err := sdk.GetInstanceFile(name, entry.path)
		if prior != nil {
			prior.Close()
		}
		if !api.StatusErrorCheck(err, http.StatusNotFound) {
			t.Fatal("synthetic destination not proven absent")
		}
		if err := sdk.CreateInstanceFile(name, entry.path, incus.InstanceFileArgs{
			Content: strings.NewReader(entry.data), UID: 70, GID: 70, Mode: 0o600, Type: "file", WriteMode: "overwrite",
		}); err != nil {
			t.Fatal("synthetic PostgreSQL file delivery failed")
		}
		body, metadata, err := sdk.GetInstanceFile(name, entry.path)
		if body != nil {
			body.Close()
		}
		if err != nil || metadata == nil || metadata.UID != 70 || metadata.GID != 70 || metadata.Mode != 0o600 || metadata.Type != "file" {
			t.Fatal("synthetic PostgreSQL file metadata mismatch")
		}
	}
	f.verifyMetadata(t, sdk, name)
}

func (f postgresFileFixture) verify(t *testing.T, ctx context.Context, sdk incus.InstanceServer, name, query string) {
	t.Helper()
	if err := execOCI(ctx, sdk, name, postgresFixtureQuery(postgresFixturePassPath, query)); err != nil {
		t.Fatal("PostgreSQL authenticated fixture query failed")
	}
	if code, err := execOCIStatus(ctx, sdk, name, postgresFixtureQuery(postgresFixtureWrongPassPath, "SELECT 1")); err != nil || code != 2 {
		t.Fatal("expected a PostgreSQL connection failure for the wrong synthetic password")
	}
	// A lost exec transport must not look like an auth denial.
	if err := execOCI(ctx, sdk, name, postgresFixtureQuery(postgresFixturePassPath, "SELECT 1")); err != nil {
		t.Fatal("PostgreSQL authentication control query failed")
	}
	f.verifyMetadata(t, sdk, name)
}

func (f postgresFileFixture) verifyMetadata(t *testing.T, sdk incus.InstanceServer, name string) {
	t.Helper()
	guest, _, err := sdk.GetInstance(name)
	if err != nil || guest == nil {
		t.Fatal("fixture instance metadata unavailable")
	}
	for _, config := range []map[string]string{guest.Config, guest.ExpandedConfig} {
		for _, value := range config {
			if strings.Contains(value, f.accepted) || strings.Contains(value, f.wrong) {
				t.Fatal("synthetic credential disclosed in Incus instance metadata")
			}
		}
	}
}

func postgresFixtureQuery(passfile, query string) []string {
	return []string{"env", "-i", "PATH=/usr/local/bin:/usr/bin:/bin", fmt.Sprintf("PGPASSFILE=%s", passfile), "psql", "-X", "-w", "-v", "ON_ERROR_STOP=1",
		"-d", "host=127.0.0.1 port=5432 user=litellm dbname=litellm require_auth=scram-sha-256 connect_timeout=3", "-tAc", query}
}
