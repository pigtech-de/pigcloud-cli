# sectest — authenticated abuse-case harness

Black-box security regression tests that drive `actions.php` directly over HTTP. This suite covers the things a DAST scanner (ZAP) **can't** reason about because they need application semantics: per-action auth gates, rate-limit enforcement, and cross-user authorization (IDOR).

It is **not** a vulnerability scanner. ZAP (`/security-scan`) finds injection/XSS/header classes; this harness asserts that the server's *own* access-control invariants hold. The two are complementary.

## Why Go / why here

The CLI authenticates with an API key (`X-Api-Key` header) — there is no SRP handshake or E2EE sealing on the wire for the request itself. That means a Go test can authenticate as a real user with one header and probe authorization without reimplementing the browser's SRP-6a client or the hybrid X25519 + ML-KEM-768 sealing. The crypto the browser fights with is already solved or simply not on this path, so the harness stays small and dependency-free (stdlib only).

## What runs

| Suite | File | Asserts |
|-------|------|---------|
| Auth gate | `authgate_test.go` | `GET` on POST-only actions → 405. Unauthenticated / bad-key POST → 401/403, never a 200 with data. `tee-attestation` requires auth. |
| Rate limit | `ratelimit_test.go` | `auth-csrf` (unauthenticated, `srp_init` bucket = 30/60s) and `account-srp-init` (authenticated, `sensitive_account` = 5/300s) return 429 **with** a `Retry-After` header once the bucket drains. |
| Authorization | `authz_test.go` | Positive control: key A authenticates (`wh` → 200, `success:true`). IDOR: key A querying `share-recipients-for-node` for a node owned by user B gets `recipients: []` — the ownership filter must not leak. Nonexistent node returns no data. |

Bucket limits mirror `private/RateLimiter.php` → `BUCKETS`. Auth-gate contracts mirror `public/cloud/actions.php` (method check precedes the auth check for `cli`, so the 405 cases need no account).

## Prerequisites

- A running PigCloud instance. **Prefer the local dev stack** (`make up` from the repo root) — these tests consume rate-limit buckets and create activity-log noise.
- One test account with an API key (`pc li`, or Settings → API key). Optionally a second account for cross-user IDOR.
- A node ID (32-hex) owned by the *second* account for the IDOR probe. Get one from `pc ls --json` / `getStructure` while logged in as B.

## Environment

| Variable | Required | Purpose |
|----------|----------|---------|
| `PIGCLOUD_SECTEST_ENDPOINT` | yes | Full `actions.php` URL, e.g. `https://localhost:8443/cloud/actions.php`. Unset → every test skips (keeps `go test ./...` green in CI). |
| `PIGCLOUD_SECTEST_KEY_A` | for authed suites | API key for account A. Unset → authenticated tests skip. |
| `PIGCLOUD_SECTEST_KEY_B` | optional | API key for account B (reserved for future cross-user write probes). |
| `PIGCLOUD_SECTEST_NODE_B` | for IDOR | A 32-hex node ID owned by account B. Unset → the IDOR probe skips. |
| `PIGCLOUD_SECTEST_ALLOW_PROD` | safety | The rate-limit suite refuses to run against a non-local endpoint unless this is `1`. Tripping `srp_init` against production throttles real logins from your IP for 60s. |
| `PIGCLOUD_SECTEST_CA_FILE` | self-signed targets | Path to a PEM cert to add to the client's trusted roots, for the local stack's self-signed TLS. Trust is scoped to this one cert — verification stays on. See below. |

## Running

From `cli/`:

PowerShell:
```powershell
$env:PIGCLOUD_SECTEST_ENDPOINT = "https://localhost:8443/cloud/actions.php"
$env:PIGCLOUD_SECTEST_KEY_A = "pc_..."
$env:PIGCLOUD_SECTEST_NODE_B = "0123abcd...32hex"
go test ./internal/sectest/ -v
```

bash:
```bash
PIGCLOUD_SECTEST_ENDPOINT="https://localhost:8443/cloud/actions.php" \
PIGCLOUD_SECTEST_KEY_A="pc_..." \
go test ./internal/sectest/ -v
```

Run one suite: `go test ./internal/sectest/ -run AuthGate -v`.

### Self-signed dev cert

The local stack serves HTTPS with a self-signed cert (HTTP on `:8080` just 301-redirects to it), so the client rejects it by default. Don't disable TLS verification — instead extract the dev cert and pin it via `PIGCLOUD_SECTEST_CA_FILE`. Verification stays on; only that one cert is added to the trust pool, so there's no MITM exposure and the OS trust store is untouched.

```powershell
docker exec pigcloud-dev-nginx-1 cat /etc/nginx/certs/cert.pem > $env:TEMP\pigcloud-dev-ca.pem
$env:PIGCLOUD_SECTEST_CA_FILE = "$env:TEMP\pigcloud-dev-ca.pem"
$env:PIGCLOUD_SECTEST_ENDPOINT = "https://localhost/cloud/actions.php"
go test ./internal/sectest/ -v
```

The cert's SANs cover `localhost` and `host.docker.internal`; use one of those hostnames in the endpoint so the name matches.

Each request retries up to 3× on transport errors (connection/timeout) with a 15s per-attempt deadline — Docker Desktop's Windows localhost forwarding intermittently stalls a connection under load, and a fresh-connection retry clears it. This mirrors the production CLI client's transient-retry behavior.

## Safety

- Default-skips everything when `PIGCLOUD_SECTEST_ENDPOINT` is unset, so it is inert under `go test ./...` and in CI.
- The rate-limit suite is local-only unless `PIGCLOUD_SECTEST_ALLOW_PROD=1`. It drains the `srp_init` bucket for your client IP; wait out the 60s window (or 300s for `sensitive_account`) before re-running.
- Read-only against user data. The IDOR probe only reads; it never writes or deletes.

## Extending

- **More auth-gate rows:** add the action name to the tables in `authgate_test.go`. Method-only cases need no account.
- **More IDOR via `?action=` + API key:** any endpoint that takes a raw node/session/link ID and authenticates by API key is a candidate — `share-recipients-for-node` is the template. Assert `requireNoLeak` plus the empty/denied shape.
- **Web-dispatch IDOR (out of scope here):** the `$_POST['action']` switch in `action_helpers.php` (e.g. `getContentIndex`, `getIndex`) authorizes against `$_SESSION["user_id"]`, not the API key, and reads FormData fields. Probing those requires a logged-in session cookie + CSRF token, which this API-key harness intentionally does not carry. Add a session-cookie variant if you want to cover that surface.
- **Deeper authz with forged crypto:** importing `pigcloud/internal/crypto` lets a test compute path tokens or sealed payloads to probe whether the server trusts client-supplied crypto material across users. Reserved for a follow-up.
