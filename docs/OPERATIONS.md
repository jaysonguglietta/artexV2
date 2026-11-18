# Operator workflows

Use [Secure local deployment](SECURE-DEPLOYMENT.md) for the supported deployment
boundary. Agent and MCP execution are disabled until explicitly enabled in an
isolated assessment VM. This guide describes functionality, not an authorization
to test a target.

## Approval records

The global approval history, task intercept-approval view, and conversation
approval cards open detailed records. The detail presentation was informed by
[AegisHook's approval component](https://github.com/RuoJi6/AegisHook/blob/main/web/src/components/CallDetail.vue)
and uses ARTEX's own components and theme.

Review records distinguish the initial decision, human decision or timeout,
effective action, and whether the tool actually executed. A model-review failure
now defaults to denial; check persisted configurations for an explicit allow-on-
failure setting before enabling execution.

## ScopeSentry asset synchronization

Configure the ScopeSentry endpoint and API key on the Asset Sync page. Select
project/task targets and asset kinds, such as domains, subdomains, IPs, ports,
sites, and endpoints. Imported records merge into company asset scopes and the
shared asset graph. Review imported scope before allowing agents to investigate;
asset presence is not proof of permission to test it. Protect the source API key
and restrict integration destinations with network policy.

## Manual vulnerability retesting

The task Retest tab lists findings with pagination, previous outcomes, and
evidence. Start a retest from a finding row or the finding detail page, optionally
providing the fixed version, conditions, and limits. A dedicated retester session
runs independently of the original scan task. The current page stays open and
shows an in-progress indicator; open the linked session for execution details.

Configure the editable `retester` agent's prompt, model, runtime budget, and tools
in Agent Management. Its model binding takes precedence over the global active
profile. Retest results are still reproducible, fixed, or inconclusive. A
successfully completed fixed result automatically sets the finding disposition to
fixed. Running, failed, stopped, or other results preserve the original status.
Original evidence and reports remain available. The status can also be changed
manually.

Only one retest session runs per finding; starting again while one is active
reuses it. After stopping, failure, or service restart, a new attempt is allowed.
Previous conclusions, evidence, and session links remain in finding details.
Retest history is not yet included in report exports or task archives, and traffic
captures are not automatically bound to retests. Mock mode produces explicitly
simulated records and does not contact actual targets.

## Remote administration and SSE

Prefer a private authenticated tunnel. If using an HTTPS reverse proxy, preserve
Host, keep frontend/API/SSE on the same origin, and disable response buffering.
Example location settings below must be placed inside a server block with valid
TLS certificates and private access controls:

```nginx
location / {
    proxy_pass http://127.0.0.1:8787;
    proxy_set_header Host $http_host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 3600s;
    proxy_http_version 1.1;
    proxy_set_header Connection "";
}
```

Do not expose the recording proxy. Cross-origin SSE and query-string bearer
tokens are unsupported. Runtime environment changes cannot modify variables
already compiled into a static frontend.

## MCP transport configuration

Remote MCP supports Streamable HTTP and legacy SSE. Legacy SSE often connects
with GET `/sse`, then receives a same-origin `/message?sessionId=...` endpoint
for JSON-RPC requests. Configure the `/sse` URL and required authentication headers.
The client rejects cross-origin handoff and redirects rather than forwarding
credentials. MCP discovery remains disabled until execution is explicitly enabled.

## Builds, updates, and backups

Build and deploy a reviewed source revision using Dockerfile.source or the source
build instructions in the main README. In-process update and rollback are disabled
by default because their fixed upstream trust and unsigned provenance are not
appropriate defaults for a personal fork. Inherited installer/update scripts are
upstream conveniences, not the supported hardened deployment path.

The schema runs idempotently at startup, including additive column/index changes.
Back up PostgreSQL, artifacts, and the private key volume before an upgrade;
rolling back a binary does not reverse schema changes. Protect retained captures,
credentials, and database backups. Never include them in a GitHub upload.

`build.sh --release` builds and embeds the frontend and packages Linux amd64/arm64,
macOS amd64/arm64, and Windows amd64 binaries. `ARTEX_TARGETS` selects platforms;
`--target` builds one target. UPX is opt-in because packed binaries can conflict
with kernels, virtualization, or security policy. Review release outputs before
publication and retain AGPL attribution.
