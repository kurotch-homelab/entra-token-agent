# Design notes

## Origin

This agent was designed as the **metrics-auth (Phase 5)** piece of the
silver-vine homelab's Keycloak → Microsoft Entra ID migration. Personal machines
(Win / macOS / Linux) run Grafana Alloy and push to a self-hosted Prometheus via
`remote_write`. The goal was to move from per-device Keycloak service-account
bearer tokens to **the actual user's Entra ID identity**, `gh auth login`-style.

## Decision: external agent, not an Alloy fork

Considered options:

| | Fork Alloy & embed device code flow | **External agent (chosen)** |
|---|---|---|
| Upkeep | Maintain a custom Alloy build forever (upstream follow, security patches, per-OS binaries) | Alloy stays vanilla & upgradable; agent released independently |
| Token rotation | Built-in logic | Atomic write to `bearer_token_file`; Alloy re-reads per request |
| Testability | Inside a huge codebase | ~single-purpose binary, easy unit tests |
| Distribution | Custom Alloy distro | Single static binary (matches Alloy's shape) |

Two verified facts made the external agent clearly correct:

1. **`prometheus.remote_write` re-reads `bearer_token_file` on every request.**
   A sidecar can rotate the token by rewriting the file — no Alloy restart.
2. **Alloy has no runtime plugin system.** New auth mechanisms require registering
   a component and importing it in the `all` package = a source fork + custom
   build. Config-based custom components (`declare`) only compose existing
   components; they cannot add new Go logic.

### Caveat

The newer `prometheus.write.queue` component lacks `bearer_token_file`
([grafana/alloy#2446](https://github.com/grafana/alloy/issues/2446)). Stick with
the classic `prometheus.remote_write` component, or re-check support before
switching.

## Token model

- **Flow**: OAuth 2.0 Device Authorization Grant, via MSAL Go
  (`github.com/AzureAD/microsoft-authentication-library-for-go`), which also owns
  the token cache.
- **Target**: model Prometheus as an Entra app registration exposing a scope
  (e.g. `metrics.write`); the user token's `aud` is validated by the OIDC proxy
  in front of Prometheus. (Entra tokens carry `aud`, unlike the current Keycloak
  tokens where oauth2-proxy checks `iss` as a stand-in.)
- **On disk**: refresh token in the OS credential store where possible (DPAPI /
  Keychain / libsecret); token file written `0600` via temp + rename.

## Failure modes to design around

- Refresh token expiry (~90d default, non-extendable) → silent stop. Emit
  `last_refresh_success_timestamp`; alert on staleness.
- Conditional Access / CAE forcing re-auth → same silent-stop risk.
- Device code flow blocked by default CA policy → needs an allow policy scoped to
  this app.

## Windows tray frontend

The Windows GUI-subsystem executable embeds the shared authentication loop;
the original cross-platform CLI remains available. Windows login launches the
tray executable directly, eliminating the persistent PowerShell console and
stderr polling. Structured authentication events drive a modeless sign-in
window; closing it does not cancel authentication, and completed or expired
requests are removed. A SID-scoped mutex matches the legacy launcher.

Alloy remains a separate LocalSystem Windows service. Its lifecycle, privileges,
configuration and token-file interface are unchanged. Exiting the tray cancels
authentication and shuts down the local HTTP listener, but does not stop Alloy.
The per-user migration script backs up launch configuration and supports rollback.

## Hybrid option

Any always-on, headless sender that cannot do interactive sign-in should use
certificate **client credentials** (machine identity) instead — keep this agent
for human-attributed, interactive devices.
