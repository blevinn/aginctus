package siwc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
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

func randomHex() string {
	data := make([]byte, 32)
	_, _ = rand.Read(data)
	return hex.EncodeToString(data)
}
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

func serialized(handler http.Handler) http.Handler {
	var mutex sync.Mutex
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		handler.ServeHTTP(w, r)
	})
}

// LoginOptions supplies presentation hooks; provider destinations remain fixed.
type LoginOptions struct {
	EnablePlan  bool
	Output      io.Writer
	OpenBrowser func(context.Context, string)
	Timeout     time.Duration
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
		options.OpenBrowser = func(ctx context.Context, address string) { _ = exec.CommandContext(ctx, "xdg-open", address).Run() }
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	return o.mutate(ctx, func(state *State) error {
		if err := o.store.write(state); err != nil {
			return err
		}
		previous := state.Accounts[account]
		clientID := "dynamic_agent_client"
		if previous != nil {
			clientID = previous.ClientID
		}
		verifier, nonce, expectedState := randomURL(48), randomURL(32), randomURL(32)
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return Error("SIWC loopback callback unavailable")
		}
		defer listener.Close()
		host := listener.Addr().String()
		redirect := "http://" + host + "/auth/callback"
		results := make(chan url.Values, 1)
		// A single accepted callback consumes the attempt, even when declined.
		accepted := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query, err := url.ParseQuery(r.URL.RawQuery)
			good := err == nil && r.Method == http.MethodGet && len(r.RequestURI) <= 16384 && !r.URL.IsAbs() && r.URL.Host == "" && r.URL.EscapedPath() == "/auth/callback" && r.Host == host
			for _, key := range []string{"state", "code", "client_id", "error"} {
				good = good && len(query[key]) <= 1
			}
			good = good && subtle.ConstantTimeCompare([]byte(query.Get("state")), []byte(expectedState)) == 1
			if !good || accepted {
				http.Error(w, "Invalid or replayed SIWC callback", 400)
				return
			}
			accepted = true
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			fmt.Fprintln(w, "Return to Aginctus. Sign-in is being validated.")
			results <- query
		})
		// Serialize handlers so simultaneous callback attempts cannot both succeed.
		server := &http.Server{Handler: serialized(handler), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 16384, ErrorLog: log.New(io.Discard, "", 0)}
		defer server.Close()
		go func() { _ = server.Serve(listener) }()
		challenge := sha256.Sum256([]byte(verifier))
		params := values("client_id", clientID, "ext_agent_host_id", state.HostID, "response_type", "code", "redirect_uri", redirect, "scope", Scopes, "resource", Resource, "state", expectedState, "nonce", nonce, "code_challenge_method", "S256", "code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
		if previous == nil {
			params.Set("agent_name_hint", "Aginctus")
		}
		if options.EnablePlan {
			params.Set("prompt", "consent")
		}
		address := Issuer + "/api/accounts/authorize?" + params.Encode()
		fmt.Fprintln(options.Output, "Continue with ChatGPT. Use the account/workspace for this Aginctus registration:\n"+address)
		// Browser launch must not delay callback processing or timeout/cancellation.
		go options.OpenBrowser(ctx, address)
		var result url.Values
		select {
		case result = <-results:
		case <-ctx.Done():
			return Error("SIWC sign-in timed out or interrupted; saved credentials preserved")
		}
		_ = server.Close()
		if result.Get("error") != "" {
			return Error("SIWC authorization declined; saved credentials preserved")
		}
		issued := result.Get("client_id")
		if _, present := result["client_id"]; !present {
			issued = clientID
		}
		if result.Get("code") == "" || issued == "dynamic_agent_client" || !clientPattern.MatchString(issued) || (previous != nil && issued != clientID) {
			return Error("SIWC callback is incomplete or returned another client")
		}
		if previous == nil {
			state.Accounts[account] = &Record{ClientID: issued}
			if err := o.store.write(state); err != nil {
				return err
			}
		}
		identity := previous
		if identity != nil && identity.Subject == "" {
			identity = nil
		}
		record, err := o.provider.tokens(ctx, values("grant_type", "authorization_code", "client_id", issued, "code", result.Get("code"), "code_verifier", verifier, "redirect_uri", redirect, "resource", Resource), identity, nonce)
		if err != nil {
			return err
		}
		for label, existing := range state.Accounts {
			if label != account && existing.ClientID == record.ClientID {
				return Error("SIWC registration already belongs to another label")
			}
		}
		state.Accounts[account] = record
		if err := o.store.write(state); err != nil {
			return err
		}
		message := "Identity only: ChatGPT plan usage was not granted."
		if slices.Contains(record.Scopes, Direct) {
			message = "ChatGPT plan usage enabled."
		}
		fmt.Fprintln(options.Output, "SIWC sign-in saved. "+message)
		return nil
	})
}
