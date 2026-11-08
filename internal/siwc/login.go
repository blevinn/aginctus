package siwc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"sync"
	"time"
)

func randomURL(size int) string {
	data := make([]byte, size)
	_, _ = rand.Read(data)
	return base64.RawURLEncoding.EncodeToString(data)
}
func values(pairs ...string) url.Values {
	result := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		result.Set(pairs[i], pairs[i+1])
	}
	return result
}

// LoginOptions supplies presentation hooks; provider destinations remain fixed.
type LoginOptions struct {
	EnablePlan  bool
	Output      io.Writer
	OpenBrowser func(context.Context, string)
	Timeout     time.Duration
}

func openSystemBrowser(ctx context.Context, address string) {
	_ = exec.CommandContext(ctx, "xdg-open", address).Run()
}

func (o *Owner) Login(ctx context.Context, account string, options LoginOptions) error {
	if !ValidName(account) {
		return Error("Use a simple account label (letters, digits, hyphen, underscore)")
	}
	if options.Output == nil {
		options.Output = io.Discard
	}
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.OpenBrowser == nil {
		options.OpenBrowser = openSystemBrowser
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	attempt := enrollment{owner: o, ctx: ctx, account: account, options: options, verifier: randomURL(48), nonce: randomURL(32)}
	return o.mutate(ctx, attempt.run)
}

type enrollment struct {
	owner                    *Owner
	ctx                      context.Context
	account, verifier, nonce string
	options                  LoginOptions
}

func (e *enrollment) run(state *State) error {
	if err := e.owner.store.write(state); err != nil {
		return err
	}
	previous := state.Accounts[e.account]
	clientID := "dynamic_agent_client"
	if previous != nil {
		clientID = previous.ClientID
	}
	result, redirect, err := e.authorize(state.HostID, clientID, previous == nil)
	if err != nil {
		return err
	}
	issued, err := issuedClient(result, clientID, previous != nil)
	if err != nil {
		return err
	}
	return e.exchange(state, previous, result.Get("code"), issued, redirect)
}

func (e *enrollment) authorize(hostID, clientID string, newClient bool) (url.Values, string, error) {
	callback, err := newLoopbackCallback()
	if err != nil {
		return nil, "", err
	}
	defer callback.close()
	challenge := sha256.Sum256([]byte(e.verifier))
	params := values("client_id", clientID, "ext_agent_host_id", hostID, "response_type", "code", "redirect_uri", callback.redirect(), "scope", Scopes, "resource", Resource, "state", callback.state, "nonce", e.nonce, "code_challenge_method", "S256", "code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	if newClient {
		params.Set("agent_name_hint", "Aginctus")
	}
	if e.options.EnablePlan {
		params.Set("prompt", "consent")
	}
	address := Issuer + "/api/accounts/authorize?" + params.Encode()
	fmt.Fprintln(e.options.Output, "Continue with ChatGPT. Use the account/workspace for this Aginctus registration:\n"+address)
	go e.options.OpenBrowser(e.ctx, address)
	select {
	case result := <-callback.results:
		return result, callback.redirect(), nil
	case <-e.ctx.Done():
		return nil, "", Error("SIWC sign-in timed out or interrupted; saved credentials preserved")
	}
}

func issuedClient(result url.Values, clientID string, returning bool) (string, error) {
	if result.Get("error") != "" {
		return "", Error("SIWC authorization declined; saved credentials preserved")
	}
	issued := result.Get("client_id")
	if _, present := result["client_id"]; !present {
		issued = clientID
	}
	if result.Get("code") == "" || issued == "dynamic_agent_client" || !clientPattern.MatchString(issued) || (returning && issued != clientID) {
		return "", Error("SIWC callback is incomplete or returned another client")
	}
	return issued, nil
}

func (e *enrollment) exchange(state *State, previous *Record, code, clientID, redirect string) error {
	if previous == nil {
		state.Accounts[e.account] = &Record{ClientID: clientID}
		if err := e.owner.store.write(state); err != nil {
			return err
		}
	}
	identity := previous
	if identity != nil && identity.Subject == "" {
		identity = nil
	}
	form := values("grant_type", "authorization_code", "client_id", clientID, "code", code, "code_verifier", e.verifier, "redirect_uri", redirect, "resource", Resource)
	record, err := e.owner.provider.tokens(e.ctx, form, identity, e.nonce)
	if err != nil {
		return err
	}
	for label, existing := range state.Accounts {
		if label != e.account && existing.ClientID == record.ClientID {
			return Error("SIWC registration already belongs to another label")
		}
	}
	state.Accounts[e.account] = record
	if err := e.owner.store.write(state); err != nil {
		return err
	}
	message := "Identity only: ChatGPT plan usage was not granted."
	if slices.Contains(record.Scopes, Direct) {
		message = "ChatGPT plan usage enabled."
	}
	fmt.Fprintln(e.options.Output, "SIWC sign-in validated. "+message)
	return nil
}

type loopbackCallback struct {
	mutex       sync.Mutex
	host, state string
	accepted    bool
	results     chan url.Values
	server      *http.Server
	listener    net.Listener
}

func newLoopbackCallback() (*loopbackCallback, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, Error("SIWC loopback callback unavailable")
	}
	callback := &loopbackCallback{host: listener.Addr().String(), state: randomURL(32), results: make(chan url.Values, 1), listener: listener}
	callback.server = &http.Server{Handler: callback, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 16384, ErrorLog: log.New(io.Discard, "", 0)}
	go func() { _ = callback.server.Serve(listener) }()
	return callback, nil
}

func (c *loopbackCallback) redirect() string { return "http://" + c.host + "/auth/callback" }
func (c *loopbackCallback) close()           { _ = c.server.Close(); _ = c.listener.Close() }

func (c *loopbackCallback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	query, err := url.ParseQuery(r.URL.RawQuery)
	good := err == nil && r.Method == http.MethodGet && len(r.RequestURI) <= 16384 && !r.URL.IsAbs() && r.URL.Host == "" && r.URL.EscapedPath() == "/auth/callback" && r.Host == c.host
	for _, key := range []string{"state", "code", "client_id", "error"} {
		good = good && len(query[key]) <= 1
	}
	good = good && subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(c.state)) == 1
	if !good || c.accepted {
		http.Error(w, "Invalid or replayed SIWC callback", 400)
		return
	}
	c.accepted = true
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	fmt.Fprintln(w, "Return to Aginctus. Sign-in is being validated.")
	c.results <- query
}
