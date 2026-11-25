package environment

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGatewayAcceptedCASAndRevocation(t *testing.T) {
	s, c := newLocalProjectFixture(t)
	ctx := t.Context()
	pg := GatewayImageSpec{Registry: "docker.io", Repository: "library/postgres", Architecture: "amd64",
		ManifestDigest: "sha256:" + strings.Repeat("a", 64), ConfigDigest: "sha256:" + strings.Repeat("b", 64), IncusFingerprint: strings.Repeat("c", 64), Alias: "pg-alias"}
	app := pg
	app.Alias, app.Repository = "llm-alias", "berriai/litellm"
	c.Gateway = &GatewaySpec{ManagementNetwork: "management", RootPool: "root", DatabasePool: "database",
		Postgres: pg, LiteLLM: app, CredentialVersion: "v3", EnsureRunning: true}
	canonical, err := c.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		strings.Replace(string(canonical), `"credentialVersion":"v3"`, `"password":"secret"`, 1),
		strings.Replace(string(canonical), `"postgres":{`, `"Postgres":{`, 1),
		strings.Replace(string(canonical), `"variant":""`, `"variant":null`, 1),
		strings.Replace(string(canonical), `"ensureRunning":true}`, `"ensureRunning":true,"EnsureRunning":true}`, 1),
	} {
		if _, err := ParseCandidate([]byte(invalid)); err == nil {
			t.Fatal("ambiguous or secret gateway intent accepted")
		}
	}
	next, err := BuildAcceptedRevision(nil, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	policy := func(_ context.Context, r LocalPromotionRequest) (Binding, error) {
		calls++
		if r.Environment != c.Environment || r.Expected != (AcceptedRevision{}) || r.CandidateDigest != next.CandidateDigest {
			t.Fatal("promotion request not bound to full candidate")
		}
		if calls == 3 {
			return Binding{}, errors.New("internal policy detail")
		}
		return c.Environment.Binding, nil
	}
	if _, err := s.PromoteLocalCandidate(ctx, c.Environment, nil, c, policy); err == nil || strings.Contains(err.Error(), "internal policy detail") {
		t.Fatalf("revocation before publication not enforced or redacted: %v", err)
	}
	if revision, published, err := s.ReadLocalAccepted(ctx, c.Environment); err != nil || revision != nil || published != nil {
		t.Fatalf("revoked CAS published: %v %v %v", revision, published, err)
	}
	if _, _, err := s.ReadLocalRevision(ctx, c.Environment, next.ID); err == nil {
		t.Fatal("orphan read as accepted")
	}
	allow := func(context.Context, LocalPromotionRequest) (Binding, error) { return c.Environment.Binding, nil }
	first, err := s.PromoteLocalCandidate(ctx, c.Environment, nil, c, allow)
	if err != nil || first != next {
		t.Fatalf("authorized CAS failed: %v %v", first, err)
	}
	tweaked, err := ParseCandidate(canonical)
	if err != nil {
		t.Fatal(err)
	}
	tweaked.Gateway.CredentialVersion = "v4"
	if first.ValidateAgainst(tweaked) == nil {
		t.Fatal("gateway version was not pinned in digest")
	}
	diff, err := CompareCandidates(c.Environment, c.Environment.Binding, c, tweaked)
	if err != nil || !diff.Changed || !diff.GatewayChanged {
		t.Fatalf("gateway-only diff missing: %+v %v", diff, err)
	}
	if _, err := s.PromoteLocalCandidate(ctx, c.Environment, nil, tweaked, allow); !errors.Is(err, ErrAcceptedConflict) {
		t.Fatalf("stale genesis CAS: %v", err)
	}
}
