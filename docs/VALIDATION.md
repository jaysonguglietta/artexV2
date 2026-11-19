# Validation record

Last follow-up: **19 November 2026**. This is a point-in-time record, with
completed observations separated from pending verification. November 18 image
IDs and hosted runs below are historical checkpoints for their exact source;
they do not establish results for the current storage/toolchain/release edits.
No local pass implies hosted CI, real OIDC or production publication passed.

## Current jq/util-linux change — fresh results pending

The operator authorized actual jq upgrading and a reviewed util-linux security upgrade
against the `3062e9c…` publication baseline. New package builds, both native
architecture scans and isolation/scanning regressions are **pending**. Earlier
`375b2c3…` passes and 85 Medium/9 Low counts remain scoped historical evidence;
they do not validate this change. A missing Ubuntu `FixedVersion` is not proof
that no upstream fix exists. See [worker security packages](WORKER-SECURITY-PATCHES.md)
for honest packaging and the required fresh evidence.

## Earlier native reviewed images — passed at `375b2c3…`

[Native image run44540981464](https://github.com/jaysonguglietta/artexV2/actions/runs/44540981464)
passed all **eight** component/architecture gates at source
`375b2c3f460d30cddfecd3437e47b543ec5b9b66`. Top-level and every image record are
`reviewed-findings`, with **zero blocking and 94 exact reviews per platform**.
Both workers retain **85 Medium/9 Low active OS records**, no High/Critical/Unknown;
application/broker/PostgreSQL report zero at all severities. These are explicit,
contained, expiring accepted risks, not repaired packages or a zero-active result.

[Baseline image evidence](../security/image-followup-evidence.json) records all
eight image/config/rootfs identities, both native scanner/DB identities, full
remaining findings and exact applied review data, and **16 verified report/SBOM
hashes**. The 188 reviews match current policy SHA256
`21649a8477f1a8e73456b1f3350ca6e8db2d97d23628c2b4aa5ead4c3c93f738`.
Trivy 0.76.2 used DB `UpdatedAt=2026-11-19T13:02:29.533007734Z`, SHA256
`923c9ac4d894e62da8020926c64e7bbff80f2562944708ac6804388eb1debec9`.
Review expiry remains `2026-12-19T12:00:00Z`. Earlier local/standard/strict proofs
are retained in explicitly historical fields; old PostgreSQL `2f4821…` is not a
current scan identity. These are native local-export IDs, not published release
digests.

| Current platform/component | Scanned image ID | Authenticated config digest |
|---|---|---|
| linux/amd64/application | `sha256:b9d7207dac7c820d67ca56b60713bf0ea839e663b7a1d49694670f747987fd01` | `sha256:b9d7207dac7c820d67ca56b60713bf0ea839e663b7a1d49694670f747987fd01` |
| linux/amd64/worker | `sha256:628abc892e913bb67fbd84cf041f11d3519df95fa95e6532d12ee540a92034f2` | `sha256:628abc892e913bb67fbd84cf041f11d3519df95fa95e6532d12ee540a92034f2` |
| linux/amd64/broker | `sha256:d9445a1575aa70c3aad2bfc952bb83af8c1be2e8bb61d2cdfe09227652d0e194` | `sha256:d9445a1575aa70c3aad2bfc952bb83af8c1be2e8bb61d2cdfe09227652d0e194` |
| linux/amd64/postgres | `sha256:d5c1110eca34642d92ab4849ce90052737b781533410b123b05d397f2f647969` | `sha256:d5c1110eca34642d92ab4849ce90052737b781533410b123b05d397f2f647969` |
| linux/arm64/application | `sha256:8b3559fb779f554d12a74cae1eec47d0bc943f160fa12c51a38315138e351b68` | `sha256:8b3559fb779f554d12a74cae1eec47d0bc943f160fa12c51a38315138e351b68` |
| linux/arm64/worker | `sha256:e293501744834e199d18e77e42dbe8145d3beb2504fa43085d0b65107d676292` | `sha256:e293501744834e199d18e77e42dbe8145d3beb2504fa43085d0b65107d676292` |
| linux/arm64/broker | `sha256:8e5603091c71c9737d737d0676d062f9563debc539d763c908fc3fe99abf9b5b` | `sha256:8e5603091c71c9737d737d0676d062f9563debc539d763c908fc3fe99abf9b5b` |
| linux/arm64/postgres | `sha256:05e97f5b2e5d3fbb5a02084ac5e91d6b4dc5e129cb29e1140b4014c4d17c2712` | `sha256:05e97f5b2e5d3fbb5a02084ac5e91d6b4dc5e129cb29e1140b4014c4d17c2712` |

| Current hosted check at `375b2c3…` | Status |
|---|---|
| Native reviewed image gates, run44540981464 | **8/8 passed**, as above |
| [Go run44540981490](https://github.com/jaysonguglietta/artexV2/actions/runs/44540981490): integration/frontend/unit/security/recon | **Passed** |
| Same run: worker/Compose/capacity/backup boundary job | **Passed**: client11/tools6/worker53/Compose17/disk16/migration7/backup22. All six Go jobs are green for this exact source. |
| [Documentation run44540981537](https://github.com/jaysonguglietta/artexV2/actions/runs/44540981537) | **Passed** at `375b2c3…`; these later documentation/evidence edits require their own checks |
| Main promotion | Fast-forwarded without force to tested `375b2c3f460d30cddfecd3437e47b543ec5b9b66` after branch CI passed |
| [Real main OIDC run44542433712](https://github.com/jaysonguglietta/artexV2/actions/runs/44542433712), attempt1 | **Passed** at `375b2c3…`: actual GitHub OIDC/Fulcio/Rekor blob/image verification and all eight negative cases. Policy/dry-run jobs succeeded; production jobs were skipped as expected. |
| Source publication | Pinned Gitleaks v8.30.1 checks the prepared source: six individually reviewed synthetic matches, zero unresolved matches. ZIP/manifest/Git bytes, modes and paths are compared before upload. The companion `.validation.json` receipt records the exact archive, manifest, commit and scan identities; it is stored outside the source tree to avoid a self-referential hash. |
| Later documentation descendants | The implementation results above bind `375b2c3…`. Check the [latest main workflows](https://github.com/jaysonguglietta/artexV2/actions?query=branch%3Amain) for any later documentation/evidence-only commit; earlier image IDs are not relabeled as newer builds. |

The preceding `af9eeb56…` hosted run passed client11/tools6/worker53/Compose17
but then failed an overly compressible database-growth assertion. The current
harness forces 128 MiB of inline digest payload and requires explicit `ENOSPC` or
SQLSTATE53100. Its new local **16/16** report SHA256 is
`578a3f856e2927377e36137931857e988275f172ad0ab5b449faa1f74fa0a5d8`;
the current hosted replacement also passed all 16 checks.
[Sanitized runtime evidence](../security/postgres-runtime-evidence.json) contains
all six downloaded passing reports: check labels/results, original report hashes
and repository source bindings only. All report passes and current source hashes
were verified. The CI boundary job retains them in `runtime-security-evidence`
for 30 days. Runtime-job and image-scan builds share source identity; their image
IDs are not asserted equal. Image
reports/SBOMs have their separate 30-day retention; raw backups are not published.

The real main rehearsal rejected wrong repository/workflow/issuer/commit/ref,
tampered signed checksums, altered archive and correctly signed provenance with
the wrong repository. Candidate artifact `14537843596` is retained seven days.
No production release, version tag, GHCR package or enabling-variable change was
created. Main repeat [Go run44542391954](https://github.com/jaysonguglietta/artexV2/actions/runs/44542391954)
and [image run44542391959](https://github.com/jaysonguglietta/artexV2/actions/runs/44542391959)
also passed for that same source, including all six Go jobs and all eight native image gates.

## Local PostgreSQL/client checkpoint at `af9eeb56…`

The final local suite passed for implementation
`af9eeb56d8852dd0f5a6c236e5cc33004770284e`, pushed only to the validation branch.
At that earlier checkpoint `main` was `e30fe09`; it has since been fast-forwarded
to tested `375b2c3…`, as recorded above. No release variable, tag or release was created.
These current results supersede the earlier PostgreSQL startup failures, while
those older runs/image IDs below remain historical checkpoints.

| Final local check | Verified result |
|---|---|
| PostgreSQL/Go integration | **869 passed, 0 failed, 1 optional provider skipped**, plus **13 additional tested packages**. The optional provider skip is not a pass. |
| Bounded database/migration/reserve | **16 disk-full + 7 migration + 3 reserve** checks passed with the revised bootstrap/client source. |
| Exact-source encrypted backup/restore | **22/22 passed**; all seven watched source hashes match the current code. Fresh report SHA256 `90370aaf077e6f6379b1c5213cab5c889931d283aaf34975d503d30a74c49eb7`. |
| Actual automatic-client privilege boundary | **11/11 passed**: real Docker healthcheck, active blocked `psql` UID/GID 70, all five capability sets zero, `NoNewPrivs=1`, exact single enforcing profile, real SQL completion, direct guarded-branch bypass refusal and cleanup. All three source hashes match current code. |
| Pure AppArmor and backup regressions | **5 AppArmor + 15 backup unit** tests passed. Pure guard tests supplement, not replace, live process evidence. |

The final local summary SHA256 is
`54065634fb9965482999b0d4c9a90fd75877b4e0b49edf8d2439272bc02e56e1`.
The 11-check client report SHA256 is
`c4829ceea1398fe2c2dc1f9bf7915c1c1536b3e0b2b192574718e889f54b014e`,
using actual image
`sha256:f7c5d55b2715aae79904eb5785b433a0234390f6a37c6240aeeddf7c0b2bb1d0`.
Later local PostgreSQL rebuild indices differed, but their canonical runtime
Config/RootFS hash remained
`d7cd706737960d2714d75b7fa1db4f34151d6ccfcd9a5501fcc326537acda3a8`.
The application/worker/broker image IDs were unchanged. Runtime equivalence is
not a new-index vulnerability scan. The current `375b2c3…` images were separately
scanned by exact identity in the native proof above.

| Historical hosted check at `af9eeb56…` | Recorded status |
|---|---|
| [Go/PostgreSQL run44538242290](https://github.com/jaysonguglietta/artexV2/actions/runs/44538242290): integration, frontend, unit, security and recon | **Passed** |
| Same run: worker/Compose boundary job | Client11/tools6/worker53/Compose17 passed; job later **failed** its old compressible disk-growth assertion. The new harness result is tracked above. |
| [Native image run44538242184](https://github.com/jaysonguglietta/artexV2/actions/runs/44538242184) | Historical run; authoritative current native results are `375b2c3…` above. |
| [Documentation run44538242357](https://github.com/jaysonguglietta/artexV2/actions/runs/44538242357) | **Passed** for that source; later documentation edits need their own check |
| OIDC/credential state at that historical checkpoint | Was pending then; current `375b2c3…` main OIDC passed above. The post-edit final source credential snapshot remains separate. |

The revised root bootstrap temporarily uses `MAC_OVERRIDE` to enter one exact
`docker-default (enforce)` profile on first exec into `setpriv`. It then drops
UID/groups/all five capability sets and sets no-new-privileges before database
work. The same guarded launcher covers automatic healthcheck, SQL/dump/restore/
readiness and container password recovery. [PostgreSQL operations](POSTGRES-BACKUP.md)
explains the tradeoff and the locally fixed PG-01 client boundary.

The earlier enforcing-stack rejection and later `pg_ctl` signal failure are
historical, not the current local outcome. Native reviewed-image
[run44532828731](https://github.com/jaysonguglietta/artexV2/actions/runs/44532828731)
passed eight images at `1ea9d66`, before this final entrypoint; it must not be
used as its new PostgreSQL image scan.

## Earlier November 19 operational checkpoints

The four authorized workstreams now have implemented controls: runtime image
remediation/gating, actual release-tag protection, bounded PostgreSQL/log storage,
and encrypted backups verified by a disposable restore. Their scope and tests
are below. The separate product review's nine-item backlog remains a proposal;
these changes do not establish the full browser assessment journey.

Final local implementation evidence below covers commit
`a2b0c63f6fa1aa632e0cf727835bfd0943d8a530` on
`security/storage-image-hardening`. That validation branch was pushed; `main`
was unchanged at this checkpoint. Native hosted CI and architecture results are
reported separately when complete. Later documentation edits require a fresh
source credential snapshot even when implementation remains identical.

| Current follow-up check | Observed result / remaining scope |
|---|---|
| Disposable PostgreSQL and Go integration | **869 passed, 0 failed, 1 optional live-provider skipped**, plus **13 additional tested suites**. No live-provider pass is claimed. |
| Analysis/portability | Isolation race check, `go vet ./...` and Windows amd64 cross-build passed. Earlier six-package race evidence is retained separately below. |
| Physical PostgreSQL capacity | **16 disk-full**, **7 explicit migration** and **3 offline recovery-reserve** checks passed. Tests cover an actual fixed filesystem and preservation/refusal paths, not a named-volume `size` option. |
| Backup/Compose exact-source integration | **22/22 passed**: fresh bounded initialization and confinement, restart persistence, isolated tool call, finite logs, authenticated encrypted snapshot, independent bounded restore/hash comparison, source preservation and cleanup. Source hashes were unchanged at verification. |
| Backup/log regressions | **15 backup unit + 8 cryptographic + 4 log-policy** tests passed. |
| Live release-tag policy | Two active rulesets verified: personal-owner creation only; updates/deletions have an empty bypass list. [Evidence](../security/release-tag-policy-evidence.json) records owner and read-only checks; no tag was created or mutated. **7 policy tests** passed. |
| Signing/publication regressions | **15 signing + 15 publication** tests passed locally. This is not a new hosted OIDC or production-release result. |
| Image policy regressions | **26 passed**, including exact review severity/`fixed_version: null`, mode boundaries and expired/stale behavior. Saved-report evaluation distinguishes reviewed acceptance from the independent strict diagnostic. The actual final four-image reviewed scan also passed, with 94 reviewed/0 blocking records. |
| Explicit npm fork | **3 static + 10 installed behavior/security** checks passed; [fixture evidence](../security/npm-fork-validation.json) authenticates source/lock/test identity. This is a source-runtime fixture, not the final full worker image. |
| Explicit pip fork | **3 static + 10 installed behavior/security** checks passed, including corrected actual-code/SBOM inventory and offline venv/ensurepip. [Fixture evidence](../security/pip-fork-validation.json) records the current patch/test hashes. |
| Embedded frontend | Current embedded build passed; no UI source change in this follow-up. The earlier 26 tests/3,086-key/typecheck results below retain their recorded scope. |
| Actual copied worker tools | **6/6 passed on the final rebuilt ARM64 worker** `sha256:859d81ed57517566bfde55f03f02137c4c50b7c29a855c1c131d416922b3f9b2`: patched Python/native modules, patched venv pip, npm dependency fix, retained tools, unused software removal and real Chromium rendering/interaction/PNG capture. This is the exact worker in the reviewed image scan below. |
| Final images and advisory counts | Four exact final ARM64 images passed **reviewed-findings** policy: **0 blocking, 94 exact reviews applied**. Worker raw inventory retains **85 Medium + 9 Low active records**, all without a listed fixed version; application/broker/PostgreSQL have zero at every severity. This is explicit authorized residual-risk acceptance, not a zero-active scan or release publication. |
| Live worker acceptance | **53/53 passed on the final rebuilt images**, including the adjacent cleanup-failure fix, urgent close, descendants, watchdog expiry and cleanup. Concurrent close waits for acknowledged Docker removal or confirmed absence. |
| Concurrent-removal race validation | Latest `go test -race ./isolation` passed in **8.176 seconds**, including the adjacent cleanup-failure fix and concurrent-close/removal regressions. The final rebuilt-image 53-check live run also passed separately. |
| Final Compose acceptance | **17/17 passed**, including bounded PostgreSQL startup/confinement/restart persistence, static API contents, isolated tool execution, log rotation, network separation and cleanup. This is separate from the 22-check backup integration. |
| Independent strict diagnostic | **Earlier expected refusal verified**: the real `--require-no-active-findings` scan of the earlier `8e4ca64…` worker checkpoint blocked **94 active records**, with zero scanner errors. This remains historical zero-active-policy negative proof; it does not test or approve the new reviewed-findings publication policy. |
| Reviewed-findings publication policy | **188 exact reviews configured**, 94 per platform (85 Medium/9 Low each), with severity and `fixed_version: null`. ARM64 review `2026-11-19T13:04:35.549172Z`; amd64 review `2026-11-19T13:19:24.496638+00:00` follows actual native report verification. All expire `2026-12-19T12:00:00Z`. Only worker Ubuntu 26.04 OS vendor-unfixed Medium/Low is eligible, with a primary Ubuntu CVE reference and concrete containment. No High/Critical/Unknown or fixable waiver. The actual final ARM64 four-image reviewed scan passed with 94 reviews applied and zero blocking; hosted amd64/arm64 standard inventory passed separately; that pending checkpoint was superseded by current `375b2c3…` native reviewed passes above. |
| Host daemon and journal policy | **Activated and tested** on the dedicated `colima-artex-test` VM: Docker and journald restarted/active; a new container inherited `local`, `10m × 3`, compression. A 41,779,200-byte stdout fixture rotated to 3,188,369 bytes while retaining its final marker. Journal configured at 256 MiB persistent/64 MiB runtime/7 days; measured usage 16.0 MiB. This does not retrofit unrelated existing containers or cap all host storage. |
| Hosted native image checkpoint | [Run44531004586 passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004586) on native amd64 and arm64, four components each, source `a2b0c63`. This was **standard mode**, not a new reviewed-mode publication result. All 16 report/SBOM hashes were verified. |
| Hosted Go/PostgreSQL checkpoint | [Run44531004483 failed](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004483) at PostgreSQL startup in integration and Compose. Hosted 53 worker and six tool checks passed, along with frontend/unit/security/recon; full hosted CI is **not green**. Diagnosis and rerun are pending. |
| Hosted reviewed-mode/OIDC follow-up at that checkpoint | Was pending then; current `375b2c3…` reviewed images/Go/docs/main OIDC subsequently passed and main was fast-forwarded. No release was enabled or published. |
| Prepared-source credential checkpoint | Gitleaks reviewed a **953-file snapshot** (952 source files plus manifest): six individually reviewed synthetic fixture matches, zero unresolved. Snapshot SHA256 `7e2368cf228aea3fa4c55bc7804198b6b35fd0114eb72534d8687589ea94814b`. This predates these documentation/evidence edits; **a fresh snapshot scan is required**, and this is not labeled the final updated-source pass. |
| Production version-tag release | **Not enabled or published.** The operator authorized the narrow review allowance, not a release-enabling variable, tag or publication. Publication still requires protected-tag identity, explicit opt-in, complete tests and `--require-reviewed-findings` on every exact image/platform. Accepted residual risk is not a package fix or zero-active result. |

The exact backup integration report SHA256 is
`88020b6f4a9bd54a415947acf0b5f753ce88d0c764cd718e9502e0dff79b4bd5`.
Its seven recorded source hashes matched at that checkpoint. Subsequent
PostgreSQL/client edits change watched sources and require fresh restore evidence. The report is private local test evidence, not an uploaded database backup.
See [database/log/backup operations](POSTGRES-BACKUP.md),
[release-tag protection](RELEASE-TAG-PROTECTION.md),
[image policy](IMAGE-SECURITY.md), [signed releases](SIGNED-RELEASES.md) and
[product readiness](PRODUCT-READINESS-REVIEW.md) for controls and limits.

## Earlier November 19 local ARM64 reviewed-image checkpoint

The historical local field in [image evidence](../security/image-followup-evidence.json)
records this earlier ARM64 `reviewed-findings` result and retains all **94 active package/advisory
records (49 unique vulnerability IDs)**, their exact risk-review records,
image/config/rootfs identity, scanner/DB/policy hashes and all eight full report/
SBOM hashes. Every report/SBOM hash and applied review was verified when refreshing
this record. Image environment and credentials are omitted. This is accepted
contained residual risk, not a repair or claim that every vulnerability is known.

| Component | Critical | High | Medium | Low | Unknown | Reviewed-mode blocking | Exact reviews applied |
|---|---:|---:|---:|---:|---:|---:|---:|
| application | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| worker | 0 | 0 | 85 | 9 | 0 | 0 | 94 |
| broker | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| postgres | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

All 94 active Ubuntu worker OS records have no `FixedVersion` in this DB and
match exact authorized reviews: CVE/package/version/severity/platform, explicit
`fixed_version: null`, primary Ubuntu reference and concrete containment. Reviews
expire `2026-12-19T12:00:00Z`. No withdrawn classification was used. There is no
amd64 acceptance inferred from this local run; the separate hosted standard
checkpoint and subsequent exact amd64 reviews are recorded below. The independent strict diagnostic's older
94-record refusal on worker `8e4ca64…` is retained in a separate historical field;
it is not a current-image strict result or failure of reviewed publication.

Trivy **0.76.2** used schema-2 DB
`UpdatedAt=2026-11-19T07:06:49.809946527Z`, SHA256
`ebba0a2f709d971b1b49f06569bb7cded629f9f87a5cf71bb5cb8dec015a07c0`.
The actual reviewed scan completed at `2026-11-19T13:09:24.514387+00:00`.
These are local ARM64 export identities, not published GHCR release digests.

| Component | Original Docker image ID | Authenticated config digest |
|---|---|---|
| application | `sha256:b0ce94112ea7e348582de3076fe5d8ff0c7fbe432f175bfc57f8f50425f386f2` | `sha256:73809ff89c70d17ee16311384ffeacaed99a7abf9b012e685a339fcedfed5e6e` |
| worker | `sha256:859d81ed57517566bfde55f03f02137c4c50b7c29a855c1c131d416922b3f9b2` | `sha256:c523ea87cbe6c4b5542c4ab713b4cbec832070ff843835af365faaacab488b54` |
| broker | `sha256:5abefcb7ca1c436f4cb0591688d62ddc723f812c3f0e962b39dbcc01c6122c9c` | `sha256:ba2d310ba7520038e39b470b0e4d4872dff7c1a0740978e15e4d6d43897934f2` |
| postgres | `sha256:2f4821fbaa775953457ef2b557292d1ef006b8e4cc06018569d78d9467afe8db` | `sha256:b38b0fc77ade8e3d4d7a10601c72d7086472be0e6068c0affa80f7bd1c708fed` |

The final implementation passed **53 live worker checks**, **six actual worker/
browser checks** on the worker above and the **22-check encrypted backup/restore
integration**. The backup report remains byte-identical at the hash above and all
seven watched source hashes matched at that checkpoint. Later PostgreSQL/client
edits need fresh restore evidence. The evidence JSON binds these local report
hashes without publishing private backup data. Initial native architecture standard scans subsequently passed; new reviewed-mode
hosted verification and PostgreSQL diagnosis were completed by the current
`375b2c3…` results above. A fresh post-documentation source credential scan remains separate. No production-release variable, version tag or publication was enabled.

## November 19 initial hosted native image checkpoint

[Image run44531004586](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004586)
checked `a2b0c63f6fa1aa632e0cf727835bfd0943d8a530`. Both native architectures
passed **standard** scans with identical 94 worker OS identities/installed versions:
85 Medium/9 Low per platform, zero High/Critical/Unknown; the other components
reported zero. This run predates the 94 new amd64 policy entries and reviewed-mode
CI configuration; it is not silently reclassified as reviewed-mode proof.

The hosted DB updated at `2026-11-19T13:02:29.533007734Z`, SHA256
`923c9ac4d894e62da8020926c64e7bbff80f2562944708ac6804388eb1debec9`.
[Evidence](../security/image-followup-evidence.json) adds both platform image/config/
rootfs bindings, scanner/DB identities and all 16 verified report/SBOM hashes, while
preserving the earlier local reviewed pass. The subsequent policy has 188 exact
reviews; differing checkpoint/current policy hashes are expected.

[Go run44531004483](https://github.com/jaysonguglietta/artexV2/actions/runs/44531004483)
failed PostgreSQL startup in integration and Compose; it is **not an all-green
result**. Hosted 53 worker/six tools and frontend/unit/security/recon passed.
Those pending checks were subsequently completed at `375b2c3…`, as recorded
above; this older failing run is preserved as history, not the current outcome.

## November 18 targeted follow-up checkpoint

| Check | Observed result | Scope and limits |
|---|---|---|
| Frontend tests | 26 passed in 8 files | Includes false/missing update capability and clipboard-failure regressions; no in-process apply/rollback call occurs. |
| Clean-source TypeScript | Passed | Temporary source tree without generated Next files; verified dependencies matched the lockfile. No dependency symlink was left in publication source. |
| English catalog | Passed, 3,086 keys | English is the only application locale. |
| Biome | 0 errors, 124 warnings, 659 informational diagnostics | Existing warnings and class-format recommendations remain. |
| Image-gate regressions | 19 passed | Tool/archive tampering, missing inventories, DB freshness, EOL OS, exact platforms, OCI/config/layer identity, active/unknown/unfixed findings, scoped expiry and live withdrawal identity/failure cases. |
| Local signing/publication regressions | 14 signing + 15 publication tests passed | Signature/metadata/path/publication negatives use local cryptographic fixtures; they do not reproduce the hosted Fulcio trust chain. |
| English source gate | 480 exact reviewed technical-data lines; zero unreviewed/stale matches | Preserves evidence, provider signatures and compatible wire/migration inputs; no whole-file exemptions. |

The minimal API contains the static Go service and certificate/time-zone/skill
data, without the worker's assessment toolchain. The worker base is Ubuntu 26.04
with reviewed Node/npm/browser tooling. Production in-process apply/rollback is
permanently refused, including with the legacy enabling flag. Read-only version
checks remain available; installation is an external signed-verification workflow.

## November 18 local follow-up checkpoint

These November 18 results cover the then-current storage/image/release source and
release UI. They predate the November 19 toolchain, hard-cap, backup and stricter
publication edits. Image identities and DB provenance are retained below.

| November 18 source check | Observed result / remaining scope |
|---|---|
| Disposable PostgreSQL integration | **869 passed, 0 failed, 1 optional live-provider skipped**, including subtests; **13 additional tested Go suites passed**. The skip is not counted as a pass. |
| Full focused races | Passed for `storage`, `traffic`, `isolation`, `evidence`, `agent` and `server`. |
| Go vet and Windows cross-build | `go vet ./...` and final windows/amd64 API cross-build passed. |
| Current source-image builds | All four passed: application, worker, broker and customized PostgreSQL; includes the current embedded static frontend. Fresh export inspection checked 30 HTML pages with no unintended CJK text. |
| Worker boundary | 52/52 live checks passed against disposable synthetic targets. |
| Detection regressions | Eight suites and three supporting gates freshly passed against the updated source and synthetic fixtures. |
| API security smoke | 12/12 passed against the rebuilt application. |
| Four runtime image gates | All linux/arm64 components passed with **0 blocking findings**; complete raw counts, required inventories, config/layer bindings and live withdrawal proof are retained below. |
| Source dependency checks | Fresh govulncheck reported 0 reachable findings; npm reported 0 findings. Neither result implies complete OS/native-engine or all-build-tag coverage. |
| Storage/context behavior | Covered by the fresh PostgreSQL suites and race runs, including new quota/retention/hold and context/transcript paths; this is not an independent forensic or deletion certification. |
| Fresh Compose startup/persistence | **11/11 passed**, including native PostgreSQL UID drop, restart persistence, absent API interpreters, API-to-broker tool execution and fixture cleanup. |
| Final prepared-source credential scan | Pinned Gitleaks v8.30.1 scanned the fresh prepared source with no network: six individually reviewed synthetic fixture matches, zero unresolved matches and zero confirmed credential leaks. Publication snapshot changes require another scan. |
| Remote build/image gates | Passed on native linux/amd64 and linux/arm64, four components each; see the exact hosted run and newer DB below. |
| Real main-only keyless rehearsal | Passed with real GitHub OIDC/Fulcio/Rekor, image signatures and all eight strict negative cases; exact commit/run below. |
| Protected-tag production release | Not claimed. Requires exact tag/commit/run evidence, hosted protections, complete gates and verified publication. |

## November 18 hosted follow-up checkpoint

These runs checked source commit `d06f7d8a28c9a562c885a85237811234018eb01c` on 18 November 2026.
Its documentation/manifest-only descendant `e30fe09` retained that implementation.
The new November 19 implementation changes are outside these runs. Consult each
run for exact checkout/job identity; a production version-tag release is not claimed.

| Check | Hosted result |
|---|---|
| Image security | [Passed on native amd64 and arm64](https://github.com/jaysonguglietta/artexV2/actions/runs/44401217153), all four components on each platform, zero blocking findings. |
| Real keyless signing | [Passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44401256931), attempt 1: real GitHub OIDC/Fulcio/Rekor signatures and image verification; eight identity/tamper/provenance negatives rejected. |
| Go / PostgreSQL / workers | [All six jobs passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44401217096), including build/vet/unit/vulnerability, isolated PostgreSQL, frontend, recon, security tooling and live worker/Compose checks. |
| Frontend | [Passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44401217040). |
| Detection suites | [Passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44401217173). |
| Documentation | [Passed](https://github.com/jaysonguglietta/artexV2/actions/runs/44401217199). |

Both hosted platforms used Trivy DB `UpdatedAt=2026-11-18T19:07:09.82030288Z`,
SHA256 `d06c4389045c2eb4a755b6a25de8c0716007e6fcf287cf9c3778dbbaab560175`.
This newer DB reflects the Picomatch withdrawal: each platform now has raw
High 0 and no withdrawal exception is needed. The older local report below
retains its raw High 1 and primary withdrawal proof; no package fix is implied.

The following counts apply independently to each architecture, not their sum.

| Component | Critical | High | Medium | Low | Unknown | Blocking |
|---|---:|---:|---:|---:|---:|---:|
| application | 0 | 0 | 0 | 0 | 0 | 0 |
| worker | 0 | 0 | 132 | 14 | 0 | 0 |
| broker | 0 | 0 | 1 | 0 | 0 | 0 |
| postgres | 0 | 0 | 0 | 0 | 0 | 0 |

Complete reports and CycloneDX SBOMs are retained in that Actions run for 30 days.
Synthetic candidate bundles are retained for seven days; private local evidence
also retains the reports, logs and hashes. These CI retention settings are
separate from the application raw-capture TTL.

| Platform/component | Original Docker image ID | Authenticated config digest |
|---|---|---|
| amd64/application | `sha256:394fb49c54af1b3ffd7c2b89de115d0f38f6b2d7d7f2fc7425ff0c973f6d00ab` | `sha256:394fb49c54af1b3ffd7c2b89de115d0f38f6b2d7d7f2fc7425ff0c973f6d00ab` |
| amd64/worker | `sha256:1b16a7c186019a269139e7cbc9f2ad916c796dcae315fa73adfec36ea35b68b1` | `sha256:1b16a7c186019a269139e7cbc9f2ad916c796dcae315fa73adfec36ea35b68b1` |
| amd64/broker | `sha256:f34fbdec8c97f5e72397f27c71ff1e13ee025a45d55897b7a8e37a8599657e64` | `sha256:f34fbdec8c97f5e72397f27c71ff1e13ee025a45d55897b7a8e37a8599657e64` |
| amd64/postgres | `sha256:ee990f3eb890514a7d4f62d6154a3dce1731fb83b9841e5c2515e2190aec543d` | `sha256:ee990f3eb890514a7d4f62d6154a3dce1731fb83b9841e5c2515e2190aec543d` |
| arm64/application | `sha256:622914d8c0c1e24061755e97987934dc87ab318ce00ab80ef08d70461af004e4` | `sha256:622914d8c0c1e24061755e97987934dc87ab318ce00ab80ef08d70461af004e4` |
| arm64/worker | `sha256:3922794e1adb9b785590b984046b569f86c32e656b6c6f321869a7e0920b34d9` | `sha256:3922794e1adb9b785590b984046b569f86c32e656b6c6f321869a7e0920b34d9` |
| arm64/broker | `sha256:d99c2ea8cb74ce54da8309049427f1331f71394de74434aa44a137db945fc3f2` | `sha256:d99c2ea8cb74ce54da8309049427f1331f71394de74434aa44a137db945fc3f2` |
| arm64/postgres | `sha256:9e1b52b48ad47295d4a01a4097c5e4b646423bb2c347c2b1d343973b180d85d6` | `sha256:9e1b52b48ad47295d4a01a4097c5e4b646423bb2c347c2b1d343973b180d85d6` |

These are scanned local export identities, not published GHCR release digests.
The signing rehearsal creates no version tag, GitHub release or GHCR package.
Production publication still requires separate authorization and current release
validation. The later active tag-policy verification is recorded above.

## November 18 local ARM64 image checkpoint

All four final **linux/arm64** gates passed. These are raw database severity
counts, not counts after hiding a withdrawn record:

| Component | Critical | High | Medium | Low | Unknown | Verified withdrawn | Blocking |
|---|---:|---:|---:|---:|---:|---:|---:|
| application | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| worker | 0 | 1 | 132 | 14 | 0 | 1 | 0 |
| broker | 0 | 0 | 1 | 0 | 0 | 0 | 0 |
| postgres | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

The worker's High is Picomatch `CVE-2026-112366`, installed version `4.0.5`.
The live official API proof at `2026-11-18T21:10:47.535590Z` matched that exact
npm/CVE identity to
[GHSA-fc5q-39xc-vjpf](https://github.com/advisories/GHSA-fc5q-39xc-vjpf), withdrawn at
`2026-11-18T15:38:56Z`. The report retains raw High 1 and the full finding/proof
as `verified_withdrawn`, distinct from a risk exception. No static exception,
`ignore-unfixed` or package-wide suppression was used. Active, mismatched,
ambiguous or unavailable evidence would still block. This is a point-in-time
advisory-status classification, not a claimed package fix. Medium/Low findings
remain visible and need continuing review.

Trivy v0.76.2 used DB schema 2 from `ghcr.io/aquasecurity/trivy-db:2`, with
`UpdatedAt=2026-11-18T13:03:23.615586156Z`. The exact DB SHA256 was
`7244f40ff53c4ac2909d8fba04aa4937354a620c380dc83bbc51df715f6cd68d`;
metadata SHA256 was
`5fe01541096db5ac8b1dcd69aebc4b6a78b7274d8b042f9809854fe4bbce4f6c`.
The DB predates the withdrawal, so a fresh primary API proof was required. Future
runs must recheck DB freshness and advisory status rather than reuse this result.

### Exact November 18 local ARM64 identities

These local export IDs can be OCI index IDs. Each is authenticated through its
selected manifest/config/layers; Trivy must match the config digest and inspected
rootfs diffIDs. They are not claims of published registry digests.

| Component | Original Docker image ID | Authenticated config digest |
|---|---|---|
| application | `sha256:e9cbc5cd52d1255a88ef41762ae2b375311c52cd95544eaf07dd7f1853558972` | `sha256:d6c37bb971d731b55fb558b94f3183a7ca8eee1e9a12b8c3e778efbc7f86d707` |
| worker | `sha256:177ffe1cb988abb7173d4f986d8b02b745edc1707f4c25e9fef38a0f611350f7` | `sha256:b6ceb4b7690ddc9e81ed811002cd750f44631ee01739b573bf8d0fdb1d58fa43` |
| broker | `sha256:e2046ed7f14b4828464c64990c956d815e7252db4e16b8e291065cc2556adfb2` | `sha256:fec563a27aeef8ac25ab19c53b79ff304bc47e25c31694f11c5299e62072a4a0` |
| postgres | `sha256:d24e3cf8e8836fca127886e9dff241283dd62aff9790d86daf4fc3f002b6601e` | `sha256:fa4630afb3a9b0df186a902f405451c4a9c9d054f6a1c67749c6705d85ccdb3a` |

The minimal application has no detected OS package inventory; its application Go
binary/module/stdlib are inventoried. Worker is Ubuntu 26.04; broker and customized
PostgreSQL use Alpine 3.24.4. PostgreSQL keeps the pinned 16.16 engine and replaces
the old Go-built privilege helper with native `su-exec`. The final Compose fixture
verified initialization, native privilege drop and persistence through a restart.

The gate was also exercised against known-vulnerable earlier broker/PostgreSQL
images and correctly blocked 33 findings in total. A passing OS-only tools base
cannot substitute for a complete component: required worker/broker Go and npm/browser
inventories are checked. The official Docker CLI exposes stdlib rather than its
full vendored Go graph, and native engine versions need separate checks; no
all-CVE or all-runtime-code guarantee is inferred. See
[image security](IMAGE-SECURITY.md) for these limits and complete gate semantics.

## Earlier completed integration checkpoint

These observations predate the latest storage/image/release changes. They remain
useful earlier regression evidence; the fresh local results above supersede their
counts for the current source. Hosted follow-up evidence is recorded separately above.

| Check | Earlier observed result | Scope and limits |
|---|---|---|
| Disposable PostgreSQL integration | 817 passed, 0 failed, 1 skipped | Six database-backed packages including subtests; ten additional package suites passed. The optional live-model test needs credentials and is not counted as passed. |
| Worker Docker boundary | 52/52 passed | Disposable worker/broker containers and synthetic local targets, with cleanup. |
| Fresh Compose startup | 8/8 passed | API/PostgreSQL/broker setup, bridge creation/reuse, authentication, tool call and cleanup. |
| API container security smoke | 12/12 passed | Sessions/cookies, same-origin requests, body bounds, host execution/update gates, query-token refusal and logout/bearer revocation. |
| Focused races, Go vet and Windows build | Passed | Default host configuration plus windows/amd64 API compilation; Linux/Docker broker is separate. |
| Application/worker/broker builds | Passed | Then-current source, including embedded static frontend; earlier IDs below. |
| Earlier frontend tests/catalog | 23 tests in 7 files; 3,080 keys | Superseded by current 26-test/3,086-key targeted checks above. |
| Earlier static frontend export | 30 HTML pages checked; no unintended CJK | Predates the new read-only release UI. |
| Upload preparation | 6 regressions passed | Exclusions, no-follow access, identity race, modes, deterministic archive and overwrite protection. |
| Worker setup | 5 regressions passed | Bridge setup/reuse, unsafe/conflicting network refusal and secret-only mode. |
| npm advisory classification | 4 regressions passed | Active/unknown/unverifiable leaves fail; verified withdrawal remains distinct from a package fix. |
| Recon helper | 7 policy and 3 Chromium tests passed | Synthetic loopback servers; networkless read-only browser fixture. |
| Detection checks | 8 suites and 3 gates passed | Synthetic Sigma/Suricata, matching/lint, ATT&CK, indicators, MISP and harness/triage/template coverage. |

## Worker adversarial coverage

The live fixture checks allowed target access while denying neighboring targets,
gateways and management networks. It verifies the tool process UID, capability
sets, no-new-privileges, read-only image and absence of API secrets or a Docker
socket. Root bootstrap authority is dropped before tool input is accepted.

Lifecycle tests cover urgent cancellation during active foreground work, refusal
to reuse closed IDs, a surviving sibling run, and detached setsid/double-fork
descendants. A separate root-owned PID1 watchdog exits after lost renewals; the
worker user cannot signal or renew it. The tests observe network activity stop
after cancellation and lease expiry.

Workspace tests exercise real tmpfs artifacts, all-sibling snapshots, conflicts,
oversized preservation failures, resumption after failure and task retirement.
Additional Go regressions reject path/inode amplification before any filesystem
mutation: at most 1,024 path bytes, 32 components and 2,048 parent directories.
Empty-directory walks are bounded and invalid snapshots cannot delete originals.
Crash tests verify owner-labeled orphan reaping and persistent replay protection
after a broker restart. These tests do not establish forensic atomicity of
snapshots taken from an actively malicious writer.

The disposable fixture uses a host-network helper to install synthetic target
addresses inside its Docker host. Run it only against a dedicated assessment/test
daemon. It never needs production target addresses or credentials.

## Earlier dependency and credential checks

Official govulncheck v1.10.0 reported no reachable findings for the then-checked Go
source graph: 52 modules and 20 root packages, Go 1.27.4, DB last modified
11 November 2026. Test-only code, alternate tags/platforms and the new final source
are not automatically covered by that earlier result.

The earlier full frontend npm audit, production frontend audit and separately
locked recon/browser helper audits reported zero. An earlier still-older raw full
frontend report had six High dependency-graph entries derived from the same
Picomatch advisory withdrawn above. The npm registry later stopped returning them.
Those six graph entries were not six independent vulnerabilities. Re-run the
recorded dependency checks against the final source; do not generalize an npm
result into OS/native-engine coverage.

Gitleaks v8.30.1 found six generic-key matches, each verified as a synthetic
masking/invalid-authentication fixture. No confirmed credential leak or unresolved
match was found in that prepared snapshot. The official pinned scanner ran locally
with no external network, read-only source/root, no Docker socket/baseline and no
whole-test-directory exclusions. Later source/Git history needs a fresh scan.

## Reproduce

Use the reviewed Go toolchain, frontend Node lockfiles, Python 3 and a dedicated
disposable Docker daemon. The PostgreSQL harness creates its own loopback-only
databases; never point fault-injection tests at a real assessment/production DB.

```sh
./scripts/test-isolated.sh
go test -race ./isolation ./agent ./cmd/tool-worker
go vet ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /tmp/artex-windows-check.exe ./cmd/artex
python3 scripts/test-prepare-upload.py
python3 scripts/test-setup-workers.py
python3 scripts/test-audit-npm.py
python3 scripts/test-image-security.py
python3 scripts/test-signed-release.py
python3 scripts/test-release-publication.py
python3 scripts/test-release-tag-policy.py
python3 scripts/test-backup.py
python3 scripts/test-backup-crypto.py
python3 scripts/test-log-policy.py
python3 scripts/test-npm-fork.py
python3 scripts/test-pip-fork.py
python3 scripts/check-english.py
python3 scripts/check-doc-links.py
npm --prefix web ci --ignore-scripts
python3 scripts/audit-npm.py
npm --prefix web run typecheck
npm --prefix web run check
npm --prefix web run check:i18n
npm --prefix web run test -- --run
npm --prefix web run build:static
python3 scripts/check-web-cjk.py
docker build --target worker -f Dockerfile.source -t artex-local:worker .
docker build --target broker -f Dockerfile.source -t artex-local:broker .
python3 scripts/test-workers.py
docker build --target application -f Dockerfile.source -t artex-local:hardened .
docker build --target postgres -f Dockerfile.source -t artex-local:postgres .
python3 scripts/test-compose.py
```

Use [image security](IMAGE-SECURITY.md) for actual exact-image scans,
[signed releases](SIGNED-RELEASES.md) for external verification and the real hosted
OIDC rehearsal, and [evidence storage](EVIDENCE-STORAGE.md) for quota/retention/hold
configuration and recovery checks. See [database/log/backup operations](POSTGRES-BACKUP.md)
for destructive fault-test prerequisites, actual encrypted disposable restore and
host activation. Those guides define the scope of their controls;
no all-CVE, forensic-atomic, certified SLSA or multi-tenant guarantee is implied.

Worker and Compose fixture reports default to private temporary directories. Both
require a disposable daemon; their host-network helper only sets synthetic test
addresses. The earlier 12-check API smoke was an additional local harness, not a
checked-in command; do not claim that exact count from the commands above alone.

## Earlier local image identities

These are content IDs from the earlier integration checkpoint, not final follow-up
images or published registry digests. Rebuilds and base/OS package changes invalidate
them as evidence for a later source revision.

| Image | Local image ID |
|---|---|
| artex-local:hardened | sha256:a874c0bd4958bccecb5f0c6229bafa25fcec67073cc588d9af5b603bbb2970fe |
| artex-local:worker | sha256:09516f614ae3443fa15034b3a0fbe41c2f90d2972a3dd4f080a1e56c22fef3e9 |
| artex-local:broker | sha256:c6e66921d70b9b65dd3e1237e221d3b1c413166ef0039ece4c92d4e29687fb8d |

See [security review](SECURITY-REVIEW.md), [the audit](SECURITY-AUDIT.md) and
[worker setup](WORKER-ISOLATION.md) for findings and supported deployment limits.
