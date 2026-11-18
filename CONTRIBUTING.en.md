# Contributing

This fork is English-only. Historical language-specific filenames remain for existing links.
Read [SECURITY.md](SECURITY.md), [the code of conduct](CODE_OF_CONDUCT.md), and the
[authorized-use notice](README.md#️-read-first--authorized-use-and-legal-notice) before contributing.
Report vulnerabilities privately; do not put exploit details or credentials into a public issue.

## Authorized use and legal responsibility

Run assessments only against systems you own or have explicit authorization to test, or against
synthetic targets in a dedicated local environment. Do not add workflows that scan unapproved
production systems. Preserve the legal notices and original attribution in the README and LICENSE.
Contributors and operators remain responsible for authorization and applicable law.

## Localization policy

The interface, API narrative, code comments, documentation, and code-owned agent prompts use
English. Only `web/messages/en.json` is an active application catalog. Do not reintroduce locale
selection or regenerate removed language catalogs. New UI strings belong in the English catalog.

Preserve bytes that have technical meaning: commands, payloads, URLs, identifiers, original evidence,
Unicode test inputs, provider-error matching text, legacy mention tokens, migration equality strings,
and detection fingerprints. An apparent non-English match in these categories is data, not untranslated
product copy. Describe the reason for preserving it in an English comment when useful.

Prompt translation can change model behavior. Review prompts as executable policy, keep attacker
content distinct from instructions, and include targeted regression checks. Passing a translation
check does not establish identical model performance; make benchmark claims only with measurements.
Existing user-authored prompts and historical evidence are never rewritten just to satisfy a text scan.

## Development environment

The application uses a Go backend and a statically exported Next.js frontend. The API owns records,
sessions and model requests; tool execution belongs to the separate worker boundary described in
[WORKER-ISOLATION.md](docs/WORKER-ISOLATION.md). Use Go compatible with `go.mod`, Node 22, Docker,
Compose and Buildx. Lockfiles and pinned build images define dependency resolution.

### Backend (Go)

```sh
go build ./...
go vet ./...
go test ./... -count=1
./scripts/test-isolated.sh
```

Database-dependent tests may skip without PostgreSQL. The isolated script creates a disposable
container and a fresh database per package, records logs, and removes the container. Never point
integration tests at a development database containing valuable data or at production.

### Frontend (web)

```sh
npm --prefix web ci --ignore-scripts
npm --prefix web run typecheck
npm --prefix web run check
npm --prefix web run check:i18n
npm --prefix web run test -- --run
npm --prefix web run build:static
python3 scripts/check-web-cjk.py
python3 scripts/audit-npm.py
```

The advisory check independently verifies upstream withdrawal status and fails closed on active,
unknown, or unavailable evidence. A withdrawn advisory does not justify ignoring future alerts on
that package. Review lint warnings; do not introduce errors or perform unrelated formatting sweeps.

### Full run (Docker Compose)

Follow [SECURE-DEPLOYMENT.md](docs/SECURE-DEPLOYMENT.md) for private local startup and
[WORKER-ISOLATION.md](docs/WORKER-ISOLATION.md) for scanning. Do not enable host execution,
mount service secrets into workers, expose the broker, or bypass the operator network policy.

## Contribution process

1. Keep the change focused and describe the concrete before/after behavior.
2. Read the existing implementation and relevant design notes, especially the
   [traffic-evidence workflow](docs/finding-traffic-evidence-en.md) when changing evidence binding.
3. Run checks appropriate to the changed components and record failures, skips, and limits honestly.
4. Run `python3 scripts/check-doc-links.py` after editing documentation or moving referenced files.
5. Follow the [PR template](.github/PULL_REQUEST_TEMPLATE.md). Include screenshots for UI changes and
   explicit compatibility/migration notes when changing stored data or protocols.
6. Add user-visible changes to the unreleased changelog; preserve historical entries as history.

## Commit messages

Use an English description with `type(scope): description`. For example:

```text
fix(worker): reject execution without an approved task network policy
docs: explain English-only catalog and preserved evidence
```

## Contributing detection rules and detection tests

This repository also keeps, in [`detections/`](detections/), rules for **defending against and
detecting** autonomous AI attacks like ARTEX. It consists of deployable [Sigma](https://sigmahq.io)
rules ([`detections/sigma/`](detections/sigma/)), network [Suricata](https://suricata.io) rules
([`detections/suricata/`](detections/suricata/)), a [MITRE ATT&CK](https://attack.mitre.org/)
coverage layer ([`detections/attack/`](detections/attack/)), and tests that reproducibly prove
these rules actually fire ([`detections/tests/`](detections/tests/)). When you add or change a
detection rule, please honor the contract below. Eight test suites mechanically enforce much of
this contract, so if you change only a rule and do not update the tests/layer, the tests fail.

- **Ground every indicator in an observable fact.** The strings, User-Agents, and behavioral
  thresholds a rule uses must be ones actually found in this repository's source, and must not be
  inferred. State the source file that is the basis for the rule (for example, the `artex-enrich/1.0`
  indicator is confirmed in `enrich/enrich.go`). The indicator-match tests
  ([`detections/tests/indicators/`](detections/tests/indicators/)) check that each indicator is
  still present in both the upstream source and the rule, so if an upstream resync changes a source
  string, the test fails unless you fix the rule along with it. If you change the machine-readable
  indicator list ([`detections/indicators/artex_indicators.csv`](detections/indicators/artex_indicators.csv)),
  also update the MISP event that carries those same indicators
  ([`detections/indicators/artex_indicators.misp.json`](detections/indicators/artex_indicators.misp.json)).
  The MISP export test ([`detections/tests/misp/`](detections/tests/misp/)) enforces that the two
  files match row by row and that the event is a valid MISP document loadable by pymisp.
- **State limitations honestly.** Write what a rule cannot catch and its false-positive potential
  in the Sigma rule's `description` and in the Suricata rule's comments. If something is a general
  hunting lead (for example, a destructive command) rather than an ARTEX-specific signature, say so,
  so that a single hit does not get used to conclude the attacker is ARTEX.
- **Pass static validation.** A Sigma rule must pass the SigmaHQ validator criteria with zero issues
  (`sigma check --validation-config detections/tests/sigma_lint/validators.yml`). Plain `sigma check`
  runs only pySigma's core validators, so SigmaHQ conventions such as title casing, field/logsource
  classification, and reference links are filtered only by this config. The four exceptions are
  SigmaHQ monorepo conventions that do not fit a standalone rule set, and their rationale is recorded
  in [`detections/tests/sigma_lint/validators.yml`](detections/tests/sigma_lint/validators.yml). A
  Suricata rule must load cleanly with `suricata -T`.
- **Ship a reproducible test with it.** Prove that the rule fires (or that its structure is valid)
  with a test under [`detections/tests/`](detections/tests/). Generate the input deterministically
  each time rather than committing binaries to the repository; assert properties that are independent
  of the engine version (firing exists, no false positives) exactly; and for figures that fluctuate
  with the version, assert a lower bound and record the baseline separately. If you add or change a
  Sigma correlation rule, the backend portability test
  ([`detections/tests/sigma_backends/`](detections/tests/sigma_backends/)) checks that the rule
  converts across multiple backends, so keep it consistent with the backend-support description in
  [`detections/README.md`](detections/README.md).
- **Update the ATT&CK layer with it.** If you add or change an `attack.*` tag on a rule, update the
  techniques and scores in [`detections/attack/artex_navigator_layer.json`](detections/attack/artex_navigator_layer.json)
  to match. The consistency test enforces a bidirectional rule↔layer match, so it fails if there is a
  rule tag missing from the layer or a layer technique missing from the rules.
- **Do not include anything that reads as attack guidance.** The detection material in this repository
  maintains a defense/detection posture only. We do not accept write-ups that aid an attack, such as
  how to carry out an exploit or techniques for evading detection.

The eight test suites run as-is with only Docker, and do not commit their artifacts to the repository.
Each script exits with a non-zero code if any single assertion fails, so it can be dropped straight
into CI or a pre-commit hook.

```bash
detections/tests/sigma/run.sh           # Sigma: sigma check + backend conversion + indicator preservation
detections/tests/sigma_match/run.sh     # Sigma: atomic rules fire on malicious sample events, stay quiet on benign
detections/tests/sigma_lint/run.sh      # Sigma: full SigmaHQ-convention validators + documented criteria
detections/tests/sigma_backends/run.sh  # Sigma portability: does a correlation rule convert across backends
detections/tests/suricata/run.sh        # Suricata: synthesize pcap → suricata -r → assert alert counts
detections/tests/attack/run.sh          # ATT&CK: bidirectional layer ↔ rule consistency
detections/tests/indicators/run.sh      # Indicators: rule's pinned indicators ↔ upstream source, both ways
detections/tests/misp/run.sh            # MISP: indicator CSV ↔ MISP event sync + pymisp validity
```

To run all eight at once, use [`detections/tests/run-all.sh`](detections/tests/run-all.sh). It runs the
eight sequentially in the same order as CI, runs the rest to the end even if an earlier suite fails, then
prints a per-suite PASS/FAIL summary, and exits with a non-zero code if any one fails. An example of
wiring this runner directly as a pre-commit hook is in the repository root's
[`.pre-commit-config.yaml`](.pre-commit-config.yaml). If you install it with `pip install pre-commit &&
pre-commit install`, the runner runs only on commits that change detection rules or the upstream source
those rules pin (the same scope as CI), catching rule/test mismatches before push. The same config file
also includes the `docs` hook that checks in-repo link/image/anchor references (the `check-doc-links.py`
from step 3 of the contribution flow above).

These eight tests are run by the repository CI
([`.github/workflows/detections.yml`](.github/workflows/detections.yml)) on every push/PR that changes
anything under `detections/`. The indicator-match test also runs when the upstream source files those
indicators point to (`enrich/`, `selfupdate/`, `guard/`, `db/`, `cmd/artex/main.go`) change, catching the
case where an upstream resync changes a User-Agent, marker, or default port and silently makes a rule
stale. So a change that updates only a rule without updating the tests/layer, a rule that breaks a SigmaHQ
convention, or a rule that is inconsistent with the source shows up red in CI before merge.

The rule index and each rule's basis and limitations are in [`detections/README.md`](detections/README.md),
and the tests' assertions and how to run them are in [`detections/tests/README.md`](detections/tests/README.md).

---

## License

This project is distributed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**. By
submitting a contribution, you are taken to **agree that your contribution is also released under
AGPL-3.0**. In particular, if you modify this project and provide it to users over a network (for
example, as an online service), you must disclose the complete corresponding source code to those
users. The full terms are in the [LICENSE](LICENSE) file.
