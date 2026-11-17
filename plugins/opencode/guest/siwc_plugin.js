// SIWC has no output-token-limit field. Disable OpenCode's generated limit
// at the client; the gateway still rejects explicit unsupported API fields.
export default async () => ({
  "chat.params": async (input, output) => {
    if (input.model.providerID !== "aginctus" || !input.model.id.startsWith("siwc-")) return
    output.maxOutputTokens = undefined
    output.temperature = undefined
    output.topP = undefined
    output.topK = undefined
    output.options.store = false
  },
})
