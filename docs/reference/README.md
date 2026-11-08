# Reference: authoritative contracts

This section describes **current interfaces and invariants**. It is not a rollout checklist: start with [getting started](../getting-started.md) and the appropriate [operator path](../gateway-operator-path.md) before executing anything that changes Incus, host networking or model credentials.

| Question | Canonical reference | Source of truth |
| --- | --- | --- |
| Which subcommand and flags actually exist? | [CLI effects](../cli.md) | Installed `aginctus help` and Cobra command tests (not a copied parser manual) |
| What does a command change, what is a dry-run, when is `--force` allowed? | [CLI effects](../cli.md) | Guarded Go command semantics; explicit operator review required |
| What are config keys, source precedence, module activation side effects and project selection semantics? | [Configuration](../configuration.md) | Go loader + NixOS/Home Manager modules |
| Which secrets and trust boundaries are enforced or remain unverified? | [Security](../security.md) | Current code, observed acceptance evidence and release limitations |
| Which gateway declaration/catalog shapes are supported? | [Gateway assets](../gateway-assets.md) and [MCP configuration](../mcp-configuration.md) | Local config compilation; declaration is not an active grant |
| What does Herdr SSH identity own? | [SSH identity](../ssh-credentials.md) | Managed identity contract; registration workflow is separately privileged |

**No duplicated flag inventory.** Update these pages only for meaningful behavior or contract changes and link to generated help for syntax; do not turn a proposal, dry-run or release note into a current feature claim.
