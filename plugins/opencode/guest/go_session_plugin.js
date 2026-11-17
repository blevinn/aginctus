// OpenCode v1.15.11 chat.headers hook: sessionID is the genuine
// conversation identity supplied by the OpenCode request pipeline.
// Scope is intentionally *only* the v0.1 Go pilot alias and custom provider.
// A gateway/proxy must independently validate what headers reach upstream.
export async function AginctusGoSession() {
  return {
    "chat.headers": async (input, output) => {
      if (input.model.providerID !== "aginctus" ||
          input.model.id !== "aginctus-go-pilot") return
      const id = input.sessionID
      if (typeof id !== "string" || !/^ses_[A-Za-z0-9_-]{8,128}$/.test(id)) {
        throw new Error("Aginctus Go: missing or invalid native OpenCode session")
      }
      // Native Go forwarding reads x-opencode-session. Pinned LiteLLM
      // v1.103.0 reads x-litellm-session-id first (unless a conflicting
      // x-litellm-trace-id is present), then generic x-*-session-id for
      // spend-log session/trace attribution. No synthetic session is made.
      // These are client-provided hints, NOT trusted acceptance attestation.
      output.headers["x-opencode-session"] = id
      output.headers["x-opencode-session-id"] = id
      output.headers["x-litellm-session-id"] = id
    },
  }
}
