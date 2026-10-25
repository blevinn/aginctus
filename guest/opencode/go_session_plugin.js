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
      output.headers["x-opencode-session"] = id
    },
  }
}
