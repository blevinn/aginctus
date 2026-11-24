package environment

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func explicitConnectionFixture(t *testing.T) ExplicitRestrictedIncusConnection {
	t.Helper()
	v, observer := restrictedIncusVerifierFixture(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { t.Fatal(err) }
	der, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{SerialNumber: big.NewInt(10),
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)},
		&x509.Certificate{SerialNumber: big.NewInt(10),
			NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour)},
		&key.PublicKey, key)
	if err != nil { t.Fatal(err) }
	privateDER, err := x509.MarshalECPrivateKey(key)
	if err != nil { t.Fatal(err) }
	return ExplicitRestrictedIncusConnection{
		URL: "https://incus.test:8443", Project: "team",
		ServerCertificatePEM: observer.conn.Certificate,
		TrustedServerSHA256: v.PinnedServerCertificateSHA256,
		ClientCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE", Bytes:der})),
		ClientPrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type:"EC PRIVATE KEY", Bytes:privateDER})),
	}
}

func TestExplicitRestrictedIncusDialerRejectsUnsafeSelectionBeforeNetwork(t *testing.T) {
	for _, tc := range []struct{
		name string
		change func(*ExplicitRestrictedIncusConnection)
	}{
		{"ambient remote",func(c *ExplicitRestrictedIncusConnection){c.URL = ""}},
		{"Unix socket",func(c *ExplicitRestrictedIncusConnection){c.URL = "unix:///var/lib/incus/unix.socket"}},
		{"plaintext",func(c *ExplicitRestrictedIncusConnection){c.URL = "http://incus.test:8443"}},
		{"URL userinfo",func(c *ExplicitRestrictedIncusConnection){c.URL = "https://operator@incus.test:8443"}},
		{"URL path",func(c *ExplicitRestrictedIncusConnection){c.URL = "https://incus.test:8443/other"}},
		{"URL query",func(c *ExplicitRestrictedIncusConnection){c.URL = "https://incus.test:8443/?project=default"}},
		{"default project",func(c *ExplicitRestrictedIncusConnection){c.Project = "default"}},
		{"missing project",func(c *ExplicitRestrictedIncusConnection){c.Project = ""}},
		{"foreign server pin",func(c *ExplicitRestrictedIncusConnection){c.TrustedServerSHA256 = strings.Repeat("f",64)}},
		{"missing server certificate",func(c *ExplicitRestrictedIncusConnection){c.ServerCertificatePEM = ""}},
		{"missing client key",func(c *ExplicitRestrictedIncusConnection){c.ClientPrivateKeyPEM = ""}},
		{"malformed client key",func(c *ExplicitRestrictedIncusConnection){c.ClientPrivateKeyPEM = "not a key"}},
		{"wrong hostname",func(c *ExplicitRestrictedIncusConnection){c.URL = "https://wrong.example:8443"}},
	} {
		t.Run(tc.name,func(t *testing.T){
			config := explicitConnectionFixture(t)
			tc.change(&config)
			sdk, err := OpenExplicitRestrictedIncusProject(context.Background(), config)
			if err == nil || sdk != nil {
				t.Fatalf("unsafe dialer selection permitted: sdk=%v err=%v",sdk,err)
			}
		})
	}
	var noContext context.Context
	if sdk, err := OpenExplicitRestrictedIncusProject(noContext, explicitConnectionFixture(t)); err == nil || sdk != nil {
		t.Fatalf("nil context permitted: sdk=%v err=%v", sdk, err)
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if sdk, err := OpenExplicitRestrictedIncusProject(ctx, explicitConnectionFixture(t)); err != context.Canceled || sdk != nil {
		t.Fatalf("canceled context permitted: sdk=%v err=%v", sdk, err)
	}
}
