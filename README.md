# entra-token-agent

> A small, `gh`-style login agent that signs a **user** in to Microsoft Entra ID
> via the OAuth 2.0 **Device Authorization Grant**, then keeps a fresh
> **bearer token** written to a file that any `bearer_token_file` consumer can read
> — Prometheus / Grafana Alloy `remote_write`, and more.

> **Status:** usable release candidate. Test in your tenant before production.

## Why

Sending metrics from personal machines (Win / macOS / Linux) to a self-hosted
Prometheus behind an OIDC-authenticating proxy usually means baking a static
secret (client secret / certificate) into every device. That is machine identity,
not *who is actually sending the metrics*.

`entra-token-agent` takes the **`gh auth login` approach** instead:

1. You run it once → it prints a code and URL (device code flow).
2. You sign in with **your own Entra ID account** in a browser.
3. The agent caches a refresh token and, from then on, periodically mints a fresh
   access token and writes it to a file (atomically).
4. Grafana Alloy / Prometheus point their `bearer_token_file` at that file and
   re-read it on every request — **no restart, no static secret on disk**.

The result: metrics are attributed to a **real person**, the device holds **no
static credential** (only a cached refresh token), and revocation is a normal
Entra sign-in-session revoke.

## Design decision: why an external agent, not a fork of Alloy

This tool exists as a **separate process** on purpose. Two facts drove it:

1. **`prometheus.remote_write` re-reads `bearer_token_file` on every request**, so
   writing a fresh token to a file rotates it with **zero downtime** — clean
   separation of concerns.
2. **Grafana Alloy has no runtime plugin system.** Adding a new auth mechanism
   (device code flow) in Go would require **forking Alloy and maintaining a custom
   build** across every release and platform — far too much upkeep for one auth
   feature.

So Alloy stays vanilla and upgradable; this agent owns the login/refresh loop.

> ⚠️ The newer WAL-based `prometheus.write.queue` component does **not** support
> `bearer_token_file` yet ([grafana/alloy#2446](https://github.com/grafana/alloy/issues/2446)).
> Use the classic `prometheus.remote_write` component with this agent.

## How it works

```
                 device code flow (one-time browser sign-in)
   you  ─────────────────────────────────────────────►  Microsoft Entra ID
                                                              │
   entra-token-agent  ◄──── refresh token (cached) ──────────┘
        │  every N min: mint access token
        ▼  atomic write (temp + rename)
   /run/entra-token-agent/token        ◄── read every request ──  Alloy / Prometheus
        │                                                          remote_write
        └── self-metric: last_refresh_success_timestamp  ──►  alert on staleness
```

## Install and run

Create an Entra **public client** app registration, enable public-client/device
code flow, grant it the target API's delegated scope, then download a release
archive or build with Go 1.23+:

```sh
go install github.com/kurotch-homelab/entra-token-agent/cmd/entra-token-agent@latest
cp entra-token-agent.example.yaml entra-token-agent.yaml
entra-token-agent -config entra-token-agent.yaml
```

On the first run, follow the URL and code printed to stderr. Later runs use the
opaque MSAL cache format stored in `cache_file`. The cache file itself is
permissioned `0600`, but is **not encrypted by this application**; keep its
directory private and rely on full-disk encryption. Remove it to force login.

The process handles SIGINT/SIGTERM, refreshes before expiration, and retries
transient acquisition errors every 30 seconds. `-version` prints its build
version.

## Configuration

```yaml
tenant_id:  "<entra-tenant-guid>"
client_id:  "<app-registration-client-id>"   # public client, device code enabled
scopes:     ["api://<prometheus-app-id>/metrics.write"]
token_file: "/run/entra-token-agent/token"    # written 0600, atomic
cache_file: "/var/lib/entra-token-agent/msal.cache" # refresh-token cache, 0600
refresh_skew: "5m"                             # refresh this long before expiry
metrics_addr: "127.0.0.1:9099"                 # exposes last_refresh_success_timestamp
```

Grafana Alloy side:

```alloy
prometheus.remote_write "central" {
  endpoint {
    url               = "https://prometheus.example/api/v1/write"
    bearer_token_file = "/run/entra-token-agent/token"
  }
}
```

## Operational notes

- **Refresh-token lifetime**: native/public client refresh tokens default to ~90
  days and cannot be extended by policy. Active use keeps them alive; long idle
  periods let them expire → re-login required.
- **Conditional Access**: Entra can block device code flow by default (anti-
  phishing). A CA policy scoped to this app must **allow** it.
- **Watch the watcher**: if refresh fails, metrics stop *silently*. Alert on
  `last_refresh_success_timestamp` going stale.
- **Health endpoints**: `GET /healthz` reports process health, `GET /readyz`
  requires a non-expired token, and `GET /metrics` exposes Prometheus metrics.
- **Cache security**: the MSAL cache contains reusable credentials. It is
  atomically written with mode `0600`; restrict the parent directory as well.

## Secret scanning

CI scans all fetched Git history with Gitleaks v8.30.1 before running the Linux
tests. The binary version and SHA-256 are pinned; detected secrets are redacted
and fail CI.

For local checks, install Gitleaks v8.30.1 from the
[official releases](https://github.com/gitleaks/gitleaks/releases/tag/v8.30.1)
and run `make secrets`. Without Make, run these commands directly:

```sh
gitleaks git --log-opts="--all" --redact=100 --no-banner .
gitleaks dir --redact=100 --no-banner .
```

The second command checks current files, including uncommitted changes. These
checks do not install a Git hook; run them before committing or pushing. CI
detects secrets after upload, so it cannot prevent the initial disclosure.
Keep token and cache files outside the repository. The default cache names and
temporary credential files are also excluded by `.gitignore`; custom paths need
their own exclusions. Gitleaks may still flag local credentials during a
directory scan, even when Git ignores them.

## Release

Every `v*` tag runs the test suite and GoReleaser, publishing checksum-protected
Linux, macOS, and Windows archives for amd64 and arm64. Run `make check` before
tagging. OS credential-store integration and native service packages remain
future hardening work.

## License

[Apache-2.0](./LICENSE).

## Background

Born out of the silver-vine homelab Keycloak → Entra ID migration
(metrics-auth phase). See `docs/design.md`.
