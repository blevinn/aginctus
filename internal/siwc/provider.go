package siwc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const Issuer = "https://auth.openai.com"
const Resource = "https://api.openai.com/v1"
const Scopes = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
const Direct = "chatgpt.tokens.use.direct"

var terminalCodes = []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"}

type providerError struct {
	status int
	code   string
}

func (e *providerError) Error() string {
	return fmt.Sprintf("SIWC provider HTTP %d: %s; retry later or check sign-in", e.status, e.code)
}

type provider struct {
	request func(context.Context, string, url.Values, string, any) error
	now     func() time.Time
}

func newProvider() *provider {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &provider{now: time.Now, request: func(ctx context.Context, address string, form url.Values, bearer string, result any) error {
		parsed, err := url.Parse(address)
		if err != nil || parsed.Scheme != "https" || (parsed.Host != "auth.openai.com" && parsed.Host != "api.openai.com") || parsed.User != nil || parsed.Fragment != "" {
			return Error("Unsafe SIWC provider destination")
		}
		method := http.MethodGet
		var body io.Reader
		if form != nil {
			method = http.MethodPost
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequestWithContext(ctx, method, address, body)
		if err != nil {
			return Error("SIWC network or response failure; credentials preserved; check connectivity")
		}
		req.Header.Set("Accept", "application/json")
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := client.Do(req)
		if err != nil {
			return Error("SIWC network or response failure; credentials preserved; check connectivity")
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxState+1))
		if err != nil || len(raw) > maxState {
			return Error("SIWC network or response failure; credentials preserved; check connectivity")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var failure struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(raw, &failure)
			if !slices.Contains(terminalCodes, failure.Error) && failure.Error != "invalid_client" {
				failure.Error = "provider_error"
			}
			return &providerError{resp.StatusCode, failure.Error}
		}
		if len(raw) == 0 {
			raw = []byte(`{}`)
		}
		if json.Unmarshal(raw, result) != nil {
			return Error("SIWC network or response failure; credentials preserved; check connectivity")
		}
		return nil
	}}
}

type discovery struct {
	Issuer     string `json:"issuer"`
	JWKS       string `json:"jwks_uri"`
	Revocation string `json:"revocation_endpoint"`
}

func (p *provider) discover(ctx context.Context) (*discovery, error) {
	var data discovery
	if err := p.request(ctx, Issuer+"/.well-known/openid-configuration", nil, "", &data); err != nil {
		return nil, err
	}
	if data.Issuer != Issuer {
		return nil, Error("SIWC discovery issuer mismatch")
	}
	for _, address := range []string{data.JWKS, data.Revocation} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Host != "auth.openai.com" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return nil, Error("Unsafe SIWC discovery endpoint")
		}
	}
	return &data, nil
}

type claims struct {
	Issuer          string          `json:"iss"`
	Audience        json.RawMessage `json:"aud"`
	Subject         string          `json:"sub"`
	Expires         float64         `json:"exp"`
	Issued          *float64        `json:"iat"`
	NotBefore       json.RawMessage `json:"nbf"`
	AuthorizedParty string          `json:"azp"`
	Nonce           string          `json:"nonce"`
	ClientID        string          `json:"client_id"`
	Scope           string          `json:"scope"`
	Email           string          `json:"email"`
}

func (p *provider) claims(ctx context.Context, token, audience, nonce string) (*claims, error) {
	rejected := Error("SIWC token signature or claims rejected")
	// Require compact RS256 JWS; embedded keys and alternate algorithms are unused.
	if len(strings.Split(token, ".")) != 3 {
		return nil, rejected
	}
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(jws.Signatures) != 1 || jws.Signatures[0].Protected.KeyID == "" {
		return nil, rejected
	}
	discovery, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	var keys jose.JSONWebKeySet
	if err := p.request(ctx, discovery.JWKS, nil, "", &keys); err != nil {
		return nil, err
	}
	matches := []jose.JSONWebKey{}
	for _, key := range keys.Keys {
		if key.KeyID == jws.Signatures[0].Protected.KeyID && (key.Use == "" || key.Use == "sig") && (key.Algorithm == "" || key.Algorithm == "RS256") && key.IsPublic() {
			matches = append(matches, key)
		}
	}
	if len(matches) != 1 {
		return nil, Error("SIWC signing key not found or ambiguous")
	}
	payload, err := jws.Verify(matches[0])
	if err != nil {
		return nil, rejected
	}
	var c claims
	if json.Unmarshal(payload, &c) != nil {
		return nil, rejected
	}
	now := float64(p.now().Unix())
	var aud string
	var audiences []string
	if json.Unmarshal(c.Audience, &aud) == nil {
		audiences = []string{aud}
	} else if json.Unmarshal(c.Audience, &audiences) != nil {
		return nil, rejected
	}
	if len(c.NotBefore) > 0 {
		var notBefore *float64
		if json.Unmarshal(c.NotBefore, &notBefore) != nil || notBefore == nil || *notBefore > now {
			return nil, rejected
		}
	}
	if c.Issuer != Issuer || c.Subject == "" || !slices.Contains(audiences, audience) || (len(audiences) > 1 && c.AuthorizedParty != audience) || c.Expires <= now || c.Issued == nil || *c.Issued > now+30 || (nonce != "" && c.Nonce != nonce) {
		return nil, rejected
	}
	return &c, nil
}

func (p *provider) tokens(ctx context.Context, form url.Values, previous *Record, nonce string) (*Record, error) {
	var data struct {
		Access   string          `json:"access_token"`
		Refresh  string          `json:"refresh_token"`
		ID       string          `json:"id_token"`
		Type     string          `json:"token_type"`
		Expires  int             `json:"expires_in"`
		Scope    *string         `json:"scope"`
		Earliest json.RawMessage `json:"earliest_refresh_at"`
	}
	if err := p.request(ctx, Issuer+"/api/accounts/oauth/token", form, "", &data); err != nil {
		return nil, err
	}
	if data.Access == "" || data.Refresh == "" || !strings.EqualFold(data.Type, "bearer") || data.Expires <= 0 || data.Expires > 86400 {
		return nil, Error("Incomplete SIWC token response; sign in again if rotation occurred")
	}
	var identity *claims
	if previous != nil {
		identity = &claims{Subject: previous.Subject, Email: previous.Email}
	}
	if data.ID != "" {
		var err error
		identity, err = p.claims(ctx, data.ID, form.Get("client_id"), nonce)
		if err != nil {
			return nil, err
		}
	} else if nonce != "" {
		return nil, Error("SIWC enrollment requires a validated ID token")
	}
	if identity == nil || (previous != nil && identity.Subject != previous.Subject) {
		return nil, Error("SIWC reauthorization returned another account; original preserved")
	}
	access, err := p.claims(ctx, data.Access, Resource, "")
	if err != nil {
		return nil, err
	}
	if access.ClientID != form.Get("client_id") || access.Subject != identity.Subject {
		return nil, Error("SIWC access token does not match the selected registration")
	}
	scopes := []string{}
	if previous != nil {
		scopes = previous.Scopes
	}
	if data.Scope != nil {
		scopes = strings.Fields(*data.Scope)
	}
	for _, scope := range scopes {
		if !slices.Contains(strings.Fields(access.Scope), scope) {
			return nil, Error("SIWC token and response scopes disagree")
		}
	}
	id := data.ID
	if id == "" && previous != nil {
		id = previous.IDToken
	}
	earliest := 0.0
	if len(data.Earliest) > 0 {
		var number *float64
		if json.Unmarshal(data.Earliest, &number) != nil || number == nil {
			return nil, Error("Invalid SIWC refresh timing")
		}
		earliest = *number
	}
	return &Record{ClientID: form.Get("client_id"), Subject: identity.Subject, Email: identity.Email, AccessToken: data.Access, RefreshToken: data.Refresh, IDToken: id, Scopes: scopes, ExpiresAt: min(float64(p.now().Unix())+float64(data.Expires), access.Expires), EarliestRefreshAt: earliest}, nil
}
