# Security Policy

## Supported versions

Only the **latest GitHub Release** is supported. The plugin's binary shim
keeps installations current automatically (opt out with
`TU_AGENT_NO_AUTO_UPDATE=1`), so fixes ship by tagging a new release —
there are no maintained older branches.

## How the binary is delivered (what you are trusting)

Installing the Claude Code plugin downloads and executes a prebuilt
`tu-agent` binary:

- The shim (`plugin/bin/tu-agent`) fetches the per-platform asset from the
  latest GitHub Release of this repository and verifies it against the
  `SHA256SUMS` file published with that release.
- If `SHA256SUMS` cannot be fetched or the checksum does not match, the
  shim **aborts without installing** and prints manual-install guidance.
  It never runs an unverified download.
- The binary is cached at `~/.tu-agent/bin/` and re-verified on update.
- You can bypass the download entirely: build from source (`make build`)
  and place the binary at `~/.tu-agent/bin/tu-agent`, or point the shim at
  your own fork via `TU_AGENT_RELEASE_REPO`.

The binary itself makes **no network calls** in its deterministic paths
(graph, memory, tests). Provider calls happen only in explicitly generative
CLI commands and can be hard-blocked with `routing: { disabled: true }` or
`TU_AGENT_NO_PROVIDER` (see the README's Configuration section).

## Reporting a vulnerability

Please report vulnerabilities **privately** — do not open a public issue.

- Preferred: [GitHub private vulnerability reporting](../../security/advisories/new)
- Alternatively: email `carlosneir4@gmail.com` with subject `[tu-agent security]`

Include reproduction steps and the release version (`tu-agent version`).
Reports of particular interest: checksum/update-channel bypasses in the
shim, path traversal or file clobbering through MCP tools or CLI flags,
command injection through generated hooks or settings, and secrets leaking
into exported memory chunks or telemetry.

This is a single-maintainer project: expect an acknowledgement within a
week and a best-effort fix in the next release. Credit is given in the
release notes unless you ask otherwise.
