# ARTEX upstream notes — English edition

This legacy filename is retained for incoming links. The application and current
documentation are English-only. Use the [main README](README.md),
[secure deployment guide](docs/SECURE-DEPLOYMENT.md), and
[operator workflows](docs/OPERATIONS.md) for current instructions.

The former translated README duplicated the screenshots, architecture diagrams,
worker trace exchange, planner todo lifecycle, setup, and development instructions
now maintained in those English guides. Its additional approval, ScopeSentry,
manual retest, reverse-proxy and release-build details are preserved in the
operator guide. The following upstream-specific notes remain distinct.

## Superseded installation and update paths

Upstream provided `install.sh` to detect/install Docker and choose either a Docker
installation or local compilation. The Docker path generated `.env`; the local
path selected PostgreSQL, generated `config.json`, built the embedded binary and
started it. Those inherited scripts are not the supported hardened setup.
Current Compose builds reviewed local source rather than pulling a floating
`autumn27/artex` image. Data and private signing keys use named volumes, not the
old blanket host bind mounts.

Upstream also distributed ZIP files with `artex`, `start.sh` (or `start.bat`),
`skills/`, and `config.example.json`. Its supervisor scripts interpreted process
exit codes to restart after updates. The in-process update flow downloaded a
platform ZIP, compared `SHA256SUMS`, ran `-h` as a smoke test, staged `artex.new`,
and exited for supervisor replacement. Failed verification discarded the staged
file; three failed starts after replacement triggered rollback to `artex.old`,
retaining the failed binary as `artex.failed`. Rollback did not reverse database
migrations. Development versions disabled this update flow.

The inherited `update.sh` offered an optional `git pull`, followed by either an
image update or local rebuild. Its floating tag defaults and fixed upstream trust
are not appropriate defaults for this personal fork. In-process update and
rollback are disabled unless explicitly opted in; use a reviewed source rebuild.
Replacing only a binary inside a container would not update its browser/nmap
toolchain and would be lost on container recreation.

Database migrations run idempotently at startup through `schema.sql`, including
additive columns and indexes. Back up the database, artifacts, and private keys
before upgrading. A binary rollback is not a database rollback.

## Upstream community and references

Upstream invited users to follow the **SecSentry** WeChat public account and send
it a private message to join its discussion group. This is historical project
contact information, not an instruction to transmit assessment data.

<img src="screenshots/wx.png" alt="SecSentry WeChat public account" width="480" />

Upstream reference: [Cairn](https://github.com/oritera/Cairn).

## Original upstream license and usage notice

The following translates the original author's notice. It is preserved separately
because it is stricter than some inherited fork documentation; preserving it does
not resolve that inconsistency or change the text of the license.

The project uses the **GNU Affero General Public License v3.0 (AGPL-3.0)**; see
[LICENSE](LICENSE). Anyone may use, modify, and distribute the project under that
license, and derivative works must remain under AGPL-3.0. When a modified version
is offered to users over a network, the corresponding complete source must be
available to those users.

The original notice says that the open-source license itself does not limit uses,
and presents the following restrictions and disclaimer as additional commitments
and a statement by the author:

**ARTEX is intended only for personal learning, source-code research, and local
technical verification, not actual testing of online systems or websites.**

Permitted activities in that notice are reading, studying and researching the
source, verifying technical principles in a locally isolated environment, and
non-offensive personal learning, academic research, and code review.

The original notice prohibits scanning, probing, exploiting, or attacking any
website, online service, or connected system, whether authorized or owned by the
user; actual penetration testing, offensive/defensive exercises, or production
use; illegal intrusion, data theft, extortion, denial of service, destructive or
criminal acts; and conduct violating applicable national or regional law.

Users are responsible for applicable cybersecurity, data-protection, and
computer-crime obligations. The original mainland-China examples name the
Cybersecurity Law, Data Security Law, Personal Information Protection Law, and
related judicial interpretations. All legal responsibility and consequences of
use rest with the user.

The project is provided **AS IS**, without express or implied warranties. The
author and contributors disclaim responsibility for direct or indirect losses,
data loss, system damage, or legal disputes arising from use, whether appropriate
or otherwise. The original notice states that downloading, installing, or using
the project constitutes acknowledgment, understanding, and agreement to those
conditions.
