package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountIsolationSelectionAndStatus(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := stateDirectory()
	if err != nil {
		t.Fatal(err)
	}
	first := &Session{ClientID: "oaiapp_first", Subject: "sub_1", Email: "first@example.invalid", Issuer: issuer, HostID: "urn:uuid:host", AccessToken: "one", RefreshToken: "refresh1", SavedAt: time.Now().UTC(), ExpiresIn: 3600}
	second := &Session{ClientID: "oaiapp_second", Subject: "sub_2", Email: "second@example.invalid", Issuer: issuer, HostID: "urn:uuid:host", AccessToken: "two", RefreshToken: "refresh2", SavedAt: time.Now().UTC(), ExpiresIn: 3600}
	if err := persistAccount(dir, first); err != nil {
		t.Fatal(err)
	}
	if err := persistAccount(dir, second); err != nil {
		t.Fatal(err)
	}
	if accountID(first) == accountID(second) {
		t.Fatal("two distinct registrations share account ID")
	}
	if err := selectAccount(dir, accountID(first)); err != nil {
		t.Fatal(err)
	}
	actual, err := activeSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Subject != first.Subject {
		t.Fatal("wrong active account")
	}
	if err := selectAccount(dir, accountID(second)); err != nil {
		t.Fatal(err)
	}
	actual, err = activeSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Subject != second.Subject {
		t.Fatal("account selection failed")
	}
	var out bytes.Buffer
	if err := Status(&out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first@example.invalid", "second@example.invalid", "Gateway provisioning: not configured"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status missing %q: %s", want, out.String())
		}
	}
	for _, secret := range []string{"refresh1", "refresh2", "one", "two"} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("credential leaked in status")
		}
	}
	selected, err := readAccount(dir, accountID(first))
	if err != nil || selected.RefreshToken != "refresh1" {
		t.Fatal("switching accounts damaged another credential")
	}
}

func TestAccountRejectsTraversalAndCorruption(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"../secret", "../../foo", "bad.json", "", "../accounts"} {
		if _, err := accountPath(dir, name); err == nil {
			t.Fatalf("accepted unsafe id %q", name)
		}
	}
	s := &Session{ClientID: "oaiapp_first", Subject: "first"}
	if err := persistAccount(dir, s); err != nil {
		t.Fatal(err)
	}
	p, err := accountPath(dir, accountID(s))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAccount(dir, accountID(s)); err == nil {
		t.Fatal("accepted world-readable OAuth credential file")
	}
}

func TestAccountRejectsSymlinkReplacementAndLockRaces(t *testing.T) {
	dir := t.TempDir()
	accounts := filepath.Join(dir, "accounts")
	if err := os.Symlink(t.TempDir(), accounts); err != nil {
		t.Fatal(err)
	}
	if _, err := accountsDir(dir); err == nil {
		t.Fatal("accepted symlink account directory")
	}
	if err := os.Remove(accounts); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := withAccountLock(dir, func() error { called = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("account lock callback not invoked")
	}
}

func TestLogoutWithoutTokensPreservesAccountRegistration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, err := stateDirectory()
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{ClientID: "oaiapp_test", Subject: "subject", Email: "user@example.invalid"}
	if err := persistAccount(dir, s); err != nil {
		t.Fatal(err)
	}
	if err := selectAccount(dir, accountID(s)); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Logout(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	actual, err := activeSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if actual.ClientID != s.ClientID || actual.Subject != s.Subject {
		t.Fatal("logout discarded registration")
	}
	if !strings.Contains(out.String(), "revocation confirmed") {
		t.Fatal("local-only logout with no token not acknowledged")
	}
}

func TestOAuthRefreshRotatesTokenAndPreservesVerifiedIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" ||
			r.Form.Get("client_id") != "oaiapp_test" ||
			r.Form.Get("refresh_token") != "old_refresh" ||
			r.Form.Get("resource") != resource {
			t.Errorf("invalid refresh form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenReply{
			AccessToken: "new_access", RefreshToken: "new_refresh",
			IDToken:   "UNVERIFIED_ID_TOKEN_SHOULD_NOT_BE_SAVED",
			TokenType: "Bearer", ExpiresIn: 3600, Scope: "offline_access chatgpt.tokens.use.direct",
		})
	}))
	defer server.Close()
	s := &Session{ClientID: "oaiapp_test", Subject: "sub", IDToken: "verified-original", RefreshToken: "old_refresh"}
	if err := refreshSession(context.Background(), server.Client(), server.URL, s); err != nil {
		t.Fatal(err)
	}
	if s.RefreshToken != "new_refresh" || s.AccessToken != "new_access" || s.IDToken != "verified-original" {
		t.Fatal("refresh rotation or verified identity preservation failed")
	}
}

func TestOAuthRefreshFailuresDoNotMutateSession(t *testing.T) {
	for _, body := range []string{
		`{"access_token":"x","token_type":"Bearer","expires_in":3600}`,
		`{"access_token":"x","refresh_token":"y","token_type":"Bearer","expires_in":3600,"scope":"openid"}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		s := &Session{ClientID: "oaiapp_test", Subject: "sub", RefreshToken: "old_refresh"}
		err := refreshSession(context.Background(), server.Client(), server.URL, s)
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid token response %s", body)
		}
		if s.RefreshToken != "old_refresh" || s.AccessToken != "" {
			t.Fatal("invalid refresh replaced credentials")
		}
	}
}

func TestOAuthLegacySessionMigrationRetiresOldSecretCopy(t *testing.T) {
	dir := t.TempDir()
	legacy := &Session{ClientID: "oaiapp_legacy", Subject: "sub_legacy", RefreshToken: "old-refresh", Issuer: issuer}
	if err := saveSession(dir, legacy); err != nil {
		t.Fatal(err)
	}
	if err := withAccountLock(dir, func() error { return migrateLegacyLocked(dir) }); err != nil {
		t.Fatal(err)
	}
	actual, err := activeSession(dir)
	if err != nil || actual.RefreshToken != legacy.RefreshToken {
		t.Fatal("legacy credential was lost during migration")
	}
	if _, err := os.Lstat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
		t.Fatal("legacy token file not removed")
	}
	if err := withAccountLock(dir, func() error { return migrateLegacyLocked(dir) }); err != nil {
		t.Fatal("repeat migration must be idempotent", err)
	}
}
