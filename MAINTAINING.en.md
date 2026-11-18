# Maintainer Guide

This is the English-only fork's maintenance runbook. Historical `.en.md`, `.ko.md`, and `.zh.md`
filenames may remain for link compatibility; they do not imply active application locale support.
Contribution rules are in [CONTRIBUTING.md](CONTRIBUTING.md).

## Upstream tracking and provenance

Retain the AGPL license and original authorship. The inherited fork recorded upstream commit
`d003372` as a historical tracking base (3 October 2026). This archive-based working copy does not
prove that every upstream change after or before that point has been reviewed; verify actual Git
history when preparing a synchronization. Do not infer provenance from a changelog alone.

Inspect upstream changes on a topic branch, using explicit repository names for GitHub commands.
Never pull or publish merely because a document contains an example command. Review behavioral,
security, dependency, schema and generated-asset changes separately before merging them.

## English-only review

The active UI catalog is `web/messages/en.json`. The runtime locale and code-owned output directive
are English. Translate new narrative comments, diagnostics, docs and prompt instructions. Do not
reintroduce old multilingual extraction scripts or locale selectors.

Preserve technical originals: evidence, payloads, URLs, identifiers, Unicode fixtures, provider matching
signatures, legacy protocol tokens and exact migration sentinels. The translation inventory is a review
queue, not a command to replace every CJK match. Confirm each exception is necessary. Existing saved
user prompts and evidence remain user data.

Prompt changes require review and targeted validation. English conversion does not establish
unchanged agent judgment, safety or coverage; performance equivalence needs a measured benchmark.
Review new target content as untrusted data, never as application instructions or authorization.

## Security boundary

The API retains database/model credentials and signing authority. Shell, filesystem, custom and MCP
execution must use the separate broker and task workers. The operator-owned network policy defines
approved target CIDRs; model-editable coverage scope is not network authorization. Consult
[WORKER-ISOLATION.md](docs/WORKER-ISOLATION.md) before changing assembly, hooks, automatic SDK
tool injection, worker capabilities, artifact sync, broker admission or lifecycle handling.

Treat the broker as trusted host-management software. Never expose it, pass its socket/token to a
worker, enable host networking for a task, or remove management-network denies to make a test pass.
The synthetic network fixture is for a dedicated disposable Docker VM only.

## Verification

Follow the [development checks](CONTRIBUTING.md#development-environment). Run database-backed
workflows with `scripts/test-isolated.sh`; a green package result with skipped database tests is not
integration verification. Run frontend types, lint, tests, English catalog validation, static export,
CJK export checks and dependency review when applicable. Record exact checks and material limits.

Run `python3 scripts/check-doc-links.py` for deterministic internal file/image/heading checks.
External link liveness is a separate nonblocking check because servers may deny HEAD, automated
User-Agents or rate-limited requests. Use the repository's GET/redirect-aware external-link checker
and review allowlist changes instead of silently declaring every 403 a dead link.

## Detection drift

Indicators must remain grounded in observable source strings. Changes to emitted User-Agents,
markers, paths or ports require synchronized rule, fixture, CSV/MISP and documentation updates.
Retain exact historical markers when old logs still need detection. Run all eight detection suites
and supporting gates with `detections/tests/run-all.sh` after relevant changes.

When adding a newly pinned source file, add it to both push and pull-request path filters in
`.github/workflows/detections.yml`; the source-indicator gate checks this relationship. Keep rule
limitations and false positives explicit. A generic command match does not attribute an incident
to ARTEX. Detection tooling/version changes need their own review; update validator exclusions only
with a concrete rationale, not merely to turn CI green.

## Dependency and release review

Keep module/npm locks, image digests and action revisions pinned as intended by the build. Review
advisories from primary sources. Never force a major downgrade, spoof a package version, or add a
package-wide ignore to obtain a clean count. The npm gate distinguishes verified withdrawals while
continuing to reject active or unverified alerts.

Build local reviewed source. Self-update remains disabled by default; inherited upstream trust and
unsigned update paths are not an appropriate release process for a personal fork. Before publishing
an image, scan OS packages, review provenance/signing and retain corresponding source and licenses.

## Clean publication

Use `scripts/prepare-upload.py` to create a new source-only review copy and checksum manifest. Inspect
that manifest before upload. Exclude `.env`, policy/credential files, keys, database/runtime data,
captures, private logs, local audit output, dependency/build trees and Git history. A packaging filter
is not a full secret scanner or a security certification. Do not include real service credentials in
fixtures, screenshots, PR descriptions or CI logs.

Document remaining architectural and operational limits. Passing tests does not establish absence
of exploitable vulnerabilities or readiness for hostile multi-tenant hosting.
