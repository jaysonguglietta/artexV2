# ARTEX V2 documentation

ARTEX V2 is a single-operator, self-hosted security-assessment fork of ARTEX and
its Global Edition. This fork keeps autonomous scanning while moving executable
tools into isolated per-run containers, hardening the API and using English for
owned interface text, prompts, comments and documentation. Use a dedicated
assessment VM and test only authorized targets.

## Start here

| Goal | Guide |
|---|---|
| Understand features and architecture | [Project overview](../README.md) and [system architecture](../README.md#system-architecture) |
| Install on macOS/Linux and complete first run | [Installation guide](INSTALLATION.md) |
| Start the local UI and database | [Secure local deployment](SECURE-DEPLOYMENT.md) |
| Enable scanning with explicit target rules | [Isolated task execution](WORKER-ISOLATION.md) |
| Review security changes and remaining risks | [Security review](SECURITY-REVIEW.md) |
| Trace findings, attack paths and remediation | [Comprehensive adversarial audit](SECURITY-AUDIT.md) |
| Inspect completed checks and reproduce them | [Validation record](VALIDATION.md) |
| Review feature maturity, journeys and proposed work | [Product readiness review, sections 12–19](PRODUCT-READINESS-REVIEW.md) |
| Review OS/language packages and SBOMs | [Runtime image security](IMAGE-SECURITY.md) |
| Verify and install a signed release | [Signed releases](SIGNED-RELEASES.md) |
| Review live owner-only, immutable release-tag policy | [Release-tag protection](RELEASE-TAG-PROTECTION.md) |
| Bound PostgreSQL/log growth and verify restores | [Database/log/backup operations](POSTGRES-BACKUP.md) |
| Review explicit worker package-manager changes | [npm security fork](../tools/npm/README.md) and [pip security fork](../tools/python/README.md) |
| Manage quotas, retention and legal holds | [Evidence storage](EVIDENCE-STORAGE.md) |
| Upload a reviewed source snapshot | [Personal GitHub upload guide](../UPLOAD.md) |

## Operate and maintain

- [Operator workflows](OPERATIONS.md): approval records, ScopeSentry synchronization,
  retesting, MCP, remote administration and backups.
- [Security policy](../SECURITY.md): report a vulnerability privately.
- [Contributing](../CONTRIBUTING.md): source checks and detection contribution rules.
- [Maintenance](../MAINTAINING.md): dependency, release and attribution expectations.
- [English conversion](ENGLISH-CONVERSION.md): English-only narrative and preserved
  evidence, Unicode fixtures, migration inputs and wire identifiers.
- [Fork changelog](../CHANGELOG.md) and
  [translated upstream history](../CHANGELOG.zh.md): distinct histories with their
  original version/date and contributor references.

## Analyze evidence and defensive signals

- [Finding traffic evidence](finding-traffic-evidence-en.md): recording and evidence
  association; worker traffic capture/import limitations remain explicit in the
  worker guide.
- [Defense and detection guide](defense-en.md): observable signals, triage and
  detection limits.
- [Detection source and tests](../detections/README.md): Sigma, Suricata and related
  fixtures, with reproducible synthetic checks.

The broker has Docker-daemon authority. Container boundaries do not protect
against a compromised broker, API, daemon, kernel or operator account. Shared
multi-tenant hosting is outside the supported design. Recorded evidence and model
output remain untrusted data even after an isolated tool run succeeds. The static
API image has no assessment toolchain; those tools belong in isolated workers.
Production in-process update/rollback is unavailable. Hosted signing/publication
status and final integrated checks are reported separately in the validation record.
Current implementation `375b2c3…` passed all six hosted Go jobs, eight native
reviewed-image gates, documentation and the real main OIDC rehearsal. Main was
fast-forwarded to that source. [Runtime evidence](../security/postgres-runtime-evidence.json)
records the six sanitized check/source/report-hash artifacts; [image evidence](../security/image-followup-evidence.json)
records every current native image/config/rootfs, all 16 report/SBOM hashes and
188 exact expiring risk reviews (94 per platform). Worker findings remain active
85 Medium/9 Low per platform; acceptance is not a package fix. No formal release,
version tag, package or enabling variable was created. A fresh final source
snapshot/credential scan follows these documentation changes. Exact source/run
identities, historical failures and separately pending same-source main repeats
are in [validation](VALIDATION.md).
