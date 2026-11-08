package siwc

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/sys/unix"
)

type fixture struct {
	t                                          *testing.T
	owner                                      *Owner
	key                                        *rsa.PrivateKey
	now                                        time.Time
	nonce, challenge, redirect, subject, scope string
	auth                                       url.Values
	failure                                    error
	bad                                        map[string]any
	refreshes                                  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	owner, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, owner: owner, key: key, now: time.Unix(1800000000, 0), subject: "fixture-subject", scope: Scopes}
	owner.provider = &provider{now: func() time.Time { return f.now }, request: f.request}
	return f
}

func (f *fixture) signed(audience string, extra map[string]any) string {
	c := map[string]any{"iss": Issuer, "aud": audience, "sub": f.subject, "iat": f.now.Unix(), "exp": f.now.Unix() + 3600}
	for key, value := range extra {
		c[key] = value
	}
	for key, value := range f.bad {
		c[key] = value
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithHeader("kid", "fixture"))
	if err != nil {
		f.t.Fatal(err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		f.t.Fatal(err)
	}
	object, err := signer.Sign(data)
	if err != nil {
		f.t.Fatal(err)
	}
	token, err := object.CompactSerialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return token
}

func (f *fixture) reply() map[string]any {
	return map[string]any{"access_token": f.signed(Resource, map[string]any{"client_id": "oaiapp_fixture", "scope": f.scope}), "id_token": f.signed("oaiapp_fixture", map[string]any{"nonce": f.nonce}), "refresh_token": "fixture-refresh-rotated", "token_type": "Bearer", "expires_in": 3600, "scope": f.scope}
}

func (f *fixture) request(_ context.Context, address string, form url.Values, bearer string, result any) error {
	var data any
	switch address {
	case Issuer + "/.well-known/openid-configuration":
		data = discovery{Issuer: Issuer, JWKS: Issuer + "/keys", Revocation: Issuer + "/revoke"}
	case Issuer + "/keys":
		data = jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "fixture", Use: "sig", Algorithm: "RS256"}}}
	case Issuer + "/api/accounts/oauth/token":
		if f.failure != nil {
			return f.failure
		}
		if form.Get("client_id") != "oaiapp_fixture" || form.Get("resource") != Resource {
			f.t.Error("token binding mismatch")
		}
		if form.Get("grant_type") == "authorization_code" {
			digest := sha256.Sum256([]byte(form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(digest[:]) != f.challenge || form.Get("redirect_uri") != f.redirect {
				f.t.Error("PKCE/redirect mismatch")
			}
		} else {
			f.refreshes++
			if form.Has("scope") || form.Get("refresh_token") == "" {
				f.t.Error("invalid refresh form")
			}
		}
		data = f.reply()
	case Resource + "/models":
		if bearer == "" {
			f.t.Error("missing model catalog bearer")
		}
		data = map[string]any{"models": []any{map[string]string{"slug": "fixture-model", "display_name": "Fixture", "visibility": "list"}, map[string]string{"slug": "hidden", "visibility": "hide"}}}
	case Issuer + "/revoke":
		if f.failure != nil {
			return f.failure
		}
		if form.Get("client_id") != "oaiapp_fixture" || form.Get("token") == "" {
			f.t.Error("invalid revocation form")
		}
		data = map[string]any{}
	default:
		f.t.Errorf("unexpected provider request %s", address)
		return Error("Unexpected provider request")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func (f *fixture) browser(transform func(url.Values), hostOverride string) func(context.Context, string) {
	return func(ctx context.Context, address string) {
		parsed, err := url.Parse(address)
		if err != nil {
			f.t.Error(err)
			return
		}
		f.auth = parsed.Query()
		f.nonce = f.auth.Get("nonce")
		f.challenge = f.auth.Get("code_challenge")
		f.redirect = f.auth.Get("redirect_uri")
		query := values("state", f.auth.Get("state"), "code", "fixture-code", "client_id", "oaiapp_fixture")
		if transform != nil {
			transform(query)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", f.redirect+"?"+query.Encode(), nil)
		if err != nil {
			f.t.Error(err)
			return
		}
		if hostOverride != "" {
			req.Host = hostOverride
		}
		client := &http.Client{Timeout: time.Second}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
	}
}

func (f *fixture) login() {
	f.t.Helper()
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), Timeout: time.Second}); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) state() *State {
	f.t.Helper()
	state, err := f.owner.store.read()
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func TestEnrollmentReturningIdentityAndCatalog(t *testing.T) {
	f := newFixture(t)
	var output bytes.Buffer
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), Output: &output, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	state := f.state()
	if f.auth.Get("client_id") != "dynamic_agent_client" || f.auth.Get("agent_name_hint") != "Aginctus" {
		t.Fatal("missing dynamic registration")
	}
	if strings.Contains(output.String(), state.Accounts["personal"].AccessToken) || strings.Contains(output.String(), "fixture-refresh") {
		t.Fatal("printed credentials")
	}
	host := state.HostID
	f.login()
	if f.auth.Get("client_id") != "oaiapp_fixture" || f.auth.Has("agent_name_hint") || f.auth.Has("id_token_hint") || f.state().HostID != host {
		t.Fatal("returning registration changed")
	}
	models, err := f.owner.Models(context.Background(), "personal")
	if err != nil || !reflect.DeepEqual(models, []Model{{"fixture-model", "Fixture"}}) {
		t.Fatalf("catalog: %v %v", models, err)
	}
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), EnablePlan: true, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if f.auth.Get("prompt") != "consent" {
		t.Fatal("missing explicit reconsent")
	}
}

func TestCallbackRejectsStateDuplicatesHostDenialAndIncompleteRegistration(t *testing.T) {
	for _, test := range []struct {
		name      string
		transform func(url.Values)
		host      string
	}{
		{"state", func(v url.Values) { v.Set("state", "wrong") }, ""},
		{"duplicate", func(v url.Values) { v.Add("state", v.Get("state")) }, ""},
		{"denied", func(v url.Values) { v.Set("error", "access_denied") }, ""},
		{"code", func(v url.Values) { v.Del("code") }, ""},
		{"client", func(v url.Values) { v.Set("client_id", "") }, ""},
		{"host", nil, "foreign.invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(test.transform, test.host), Timeout: 100 * time.Millisecond})
			if err == nil || f.state().Accounts["personal"] != nil {
				t.Fatal("accepted invalid callback")
			}
		})
	}
	f := newFixture(t)
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: func(context.Context, string) {}, Timeout: time.Millisecond}); err == nil {
		t.Fatal("no timeout")
	}
}

func TestClaimsFailClosedAndReturningLoginPreservesSession(t *testing.T) {
	f := newFixture(t)
	for _, bad := range []map[string]any{{"iss": "https://evil.invalid"}, {"aud": "other"}, {"nonce": "wrong"}, {"exp": 1}, {"iat": f.now.Unix() + 3600}, {"sub": ""}, {"nbf": f.now.Unix() + 3600}, {"nbf": nil}, {"aud": []string{"oaiapp_fixture", "other"}}, {"exp": nil}} {
		f.bad = bad
		if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), Timeout: time.Second}); err == nil {
			t.Fatalf("accepted bad claims: %v", bad)
		}
		if f.state().Accounts["personal"].AccessToken != "" {
			t.Fatal("persisted invalid session")
		}
	}
	f.bad = nil
	f.login()
	original := *f.state().Accounts["personal"]
	f.subject = "different-subject"
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), Timeout: time.Second}); err == nil {
		t.Fatal("accepted another account")
	}
	if !reflect.DeepEqual(original, *f.state().Accounts["personal"]) {
		t.Fatal("destroyed original session")
	}
	token := f.signed("oaiapp_fixture", nil)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f.key = key
	if _, err := f.owner.provider.claims(context.Background(), token, "oaiapp_fixture", ""); err == nil {
		t.Fatal("accepted invalid signature")
	}
}

func TestExchangeFailureRetainsRegistrationAndIdentityOnlyCannotInvoke(t *testing.T) {
	f := newFixture(t)
	f.failure = &providerError{400, "invalid_grant"}
	if err := f.owner.Login(context.Background(), "personal", LoginOptions{OpenBrowser: f.browser(nil, ""), Timeout: time.Second}); err == nil {
		t.Fatal("exchange succeeded")
	}
	if !reflect.DeepEqual(f.state().Accounts["personal"], &Record{ClientID: "oaiapp_fixture"}) {
		t.Fatal("did not retain registration only")
	}
	f.failure = nil
	f.scope = "openid profile email"
	f.login()
	if _, err := f.owner.Models(context.Background(), "personal"); err == nil {
		t.Fatal("identity-only account invoked provider")
	}
}

func TestRefreshRotationAndAmbiguousRecovery(t *testing.T) {
	for _, test := range []struct {
		name             string
		failure          error
		pending, cleared bool
	}{
		{"success", nil, false, false},
		{"temporary", &providerError{503, "provider_error"}, false, false},
		{"terminal", &providerError{400, "refresh_token_reused"}, false, true},
		{"ambiguous", Error("Network ambiguity"), true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.login()
			f.now = f.now.Add(3601 * time.Second)
			f.failure = test.failure
			_, err := f.owner.Models(context.Background(), "personal")
			if (err != nil) != (test.failure != nil) {
				t.Fatalf("unexpected refresh error: %v", err)
			}
			record := f.state().Accounts["personal"]
			if record.RefreshPending != test.pending || (record.RefreshToken == "") != test.cleared {
				t.Fatal("unsafe refresh recovery")
			}
			if test.failure == nil {
				if f.refreshes != 1 {
					t.Fatal("did not refresh")
				}
				if _, err := f.owner.Models(context.Background(), "personal"); err != nil || f.refreshes != 1 {
					t.Fatal("did not reuse refreshed session")
				}
			}
			if test.pending {
				f.failure = nil
				if _, err := f.owner.Models(context.Background(), "personal"); err == nil {
					t.Fatal("replayed uncertain refresh")
				}
			}
		})
	}
}

func TestRuntimeLeaseBlocksEveryMutatingWorkflow(t *testing.T) {
	f := newFixture(t)
	f.login()
	release, err := f.owner.store.lock(context.Background(), "runtime", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, action := range []func() error{
		func() error { _, err := f.owner.Init(context.Background()); return err },
		func() error { return f.owner.Login(context.Background(), "personal", LoginOptions{}) },
		func() error { _, err := f.owner.Logout(context.Background(), "personal"); return err },
		func() error { _, err := f.owner.Models(context.Background(), "personal"); return err },
	} {
		if err := action(); err == nil || !strings.Contains(err.Error(), "owner busy") {
			t.Fatalf("ignored lease: %v", err)
		}
	}
	if _, err := f.owner.Status(context.Background()); err != nil {
		t.Fatal("read-only status blocked by runtime")
	}
}

func TestUnconfirmedRevocationStillClearsLocalTokens(t *testing.T) {
	f := newFixture(t)
	f.login()
	f.failure = &providerError{400, "provider_error"}
	confirmed, err := f.owner.Logout(context.Background(), "personal")
	if err != nil || confirmed {
		t.Fatalf("unexpected remote revocation result: %v %v", confirmed, err)
	}
	record := f.state().Accounts["personal"]
	if record.AccessToken != "" || record.RefreshToken != "" || record.IDToken != "" || record.ClientID != "oaiapp_fixture" {
		t.Fatal("unconfirmed revocation did not clear tokens and retain registration")
	}
}

func TestUnsafeStateAndSanitizedDiagnostics(t *testing.T) {
	f := newFixture(t)
	f.login()
	path := filepath.Join(f.owner.store.Root, "state.json")
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.store.read(); err == nil {
		t.Fatal("read public state")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.owner.store.Root, "link")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.store.read(); err == nil {
		t.Fatal("read hardlinked state")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(link, path); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.store.read(); err == nil {
		t.Fatal("read symlink")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.store.read(); err == nil {
		t.Fatal("read FIFO")
	}
	if strings.Contains(Diagnostic(errors.New("secret")), "secret") {
		t.Fatal("leaked arbitrary error")
	}
	if _, err := OpenStore(f.owner.store.Root + "/.."); err == nil {
		t.Fatal("accepted unnormalized directory")
	}
	var result map[string]any
	if err := newProvider().request(context.Background(), "http://auth.openai.com/token", nil, "", &result); err == nil {
		t.Fatal("accepted unsafe provider URL")
	}
}

func TestGoPythonStateAndLockCompatibility(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "scripts", "gateway", "siwc.py")); os.IsNotExist(err) {
		t.Skip("runtime interoperability is supplied by the next stack layer")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python compatibility probe requires test environment")
	}
	f := newFixture(t)
	f.login()
	// No Python crypto dependency is needed for this shared persistence probe.
	cmd := exec.Command(python, "-c", `import sys; sys.path.insert(0, sys.argv[1]); import siwc
s=siwc.Store(sys.argv[2])
with s.lock('runtime', timeout=0), s.lock():
    state=s.read()
    assert state['accounts']['personal']['client_id']=='oaiapp_fixture'
    state['accounts']['personal']['refresh_pending']=True
    s.write(state)
    print('locked', flush=True)
    sys.stdin.readline()
`, filepath.Join("..", "..", "scripts", "gateway"), f.owner.store.Root)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close(); _ = cmd.Wait() }()
	line := make([]byte, 7)
	if _, err := io.ReadFull(output, line); err != nil {
		t.Fatalf("Python probe failed: %v %s", err, stderr.String())
	}
	if _, err := f.owner.Models(context.Background(), "personal"); err == nil || !strings.Contains(err.Error(), "owner busy") {
		t.Fatal("Go ignored Python runtime lock")
	}
	if !f.state().Accounts["personal"].RefreshPending {
		t.Fatal("Go did not read Python state write")
	}
}

func TestGoEnrollmentPythonRefreshGoLogout(t *testing.T) {
	if _, err := os.Stat(filepath.Join("..", "..", "scripts", "gateway", "siwc.py")); os.IsNotExist(err) {
		t.Skip("runtime interoperability is supplied by the next stack layer")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python integration probe requires test environment")
	}
	if err := exec.Command(python, "-c", "import jwt, cryptography").Run(); err != nil {
		t.Skip("Python runtime crypto supplied by development shell")
	}
	f := newFixture(t)
	f.login()
	f.now = f.now.Add(3601 * time.Second)
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "fixture", Use: "sig", Algorithm: "RS256"}}}
	data, err := json.Marshal(map[string]any{"now": f.now.Unix(), "reply": f.reply(), "keys": keys})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(python, "-c", `import json,sys;sys.path.insert(0,sys.argv[1]);import siwc
data=json.load(sys.stdin)
def request(url,form=None,bearer=None):
    if url.endswith('openid-configuration'):
        return {'issuer':siwc.ISSUER,'jwks_uri':siwc.ISSUER+'/keys','revocation_endpoint':siwc.ISSUER+'/revoke'}
    if url.endswith('/keys'): return data['keys']
    assert url.endswith('/oauth/token') and form['grant_type']=='refresh_token'
    assert form['client_id']=='oaiapp_fixture' and form['resource']==siwc.RESOURCE
    return data['reply']
store=siwc.Store(sys.argv[2])
owner=siwc.Owner(store,siwc.Provider(request,lambda:data['now']))
with store.lock('runtime',timeout=0):
    assert owner.access('personal')==data['reply']['access_token']
`, filepath.Join("..", "..", "scripts", "gateway"), f.owner.store.Root)
	cmd.Stdin = bytes.NewReader(data)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Python refresh rejected Go enrollment: %v %s", err, output)
	}
	if record := f.state().Accounts["personal"]; record.RefreshPending || record.ExpiresAt <= float64(f.now.Unix()) {
		t.Fatal("Go could not read Python refresh")
	}
	confirmed, err := f.owner.Logout(context.Background(), "personal")
	if err != nil || !confirmed {
		t.Fatalf("Go logout after Python refresh: %v %v", confirmed, err)
	}
}
