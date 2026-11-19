# Release tag protection

The personal repository `jaysonguglietta/artexV2` has two additive, active GitHub
tag rulesets targeting exactly `refs/tags/v*`, with no excluded tags:

| Ruleset | Enforced rules | Bypass |
| --- | --- | --- |
| [ARTEX release tags: immutable](https://github.com/jaysonguglietta/artexV2/rules/27587325) | Restrict updates and deletions | None, including the owner |
| [ARTEX release tags: owner creation](https://github.com/jaysonguglietta/artexV2/rules/27587326) | Restrict creation | Only personal owner `jaysonguglietta` (User ID `15835881`), creation ruleset only |

Separating the rulesets matters: the owner's permission to create a version tag
does not grant permission to move or delete an existing one. Other repository
writers, deploy keys and GitHub Apps have no creation bypass. The owner can still
administer repository rules; this protects tag operations under the active policy,
not against a compromised repository administrator changing that policy.

Both rulesets were created and then read back through the REST API using the
explicit personal account. GitHub accepted the individual `User` actor and
reported active enforcement for this public repository. Existing unrelated
repository settings were preserved. No tag or release was created, updated or
deleted, and `ARTEX_ENABLE_RELEASES` remained absent. Protection does **not** opt
the repository into release publication.

The [redacted readback evidence](../security/release-tag-policy-evidence.json)
contains the public policy, IDs and verification scope. It contains no credential
or private host path. It is a historical snapshot; use the live verifier to check
current settings.

## Read-only checks and maintenance

Use a trusted checkout of the [policy helper](../scripts/release-tag-policy.py):

```sh
python3 -I scripts/release-tag-policy.py plan
python3 -I scripts/release-tag-policy.py verify --personal-account
python3 -I scripts/release-tag-policy.py verify --runtime --ref refs/tags/v1.2.3
python3 -I scripts/test-release-tag-policy.py
```

`plan` prints the two reviewed payloads without contacting GitHub. Owner
verification reads only the `jaysonguglietta` credential with
`gh auth token --hostname github.com --user jaysonguglietta`; the token stays in
process memory and the global active `gh` account is not changed. Runtime
verification uses a read-only token when supplied, or the public API. All API
requests use the fixed GitHub host/repository, bounded JSON and no redirects.

If these specific rules are missing, an authorized owner can explicitly run:

```sh
python3 -I scripts/release-tag-policy.py apply --personal-account
```

Apply is idempotent: it only adds missing policies with the two exact managed
names. It never edits/deletes other rulesets or enables releases. An existing
managed name with different settings causes refusal before any change; review
that difference rather than silently overwriting it. Applying again to the live
configured repository was also checked through a wrapper that refused every
mutation.

## What runtime verification proves

The release workflow still requires a protected version tag, explicit publication
opt-in, the exact repository/workflow and source commit, current tag identity and
ancestry in `origin/main`. It additionally reads both tag rulesets and requires
active enforcement, exact tag scope/no exclusions, expected source and the
creation/update/deletion rules. Missing, disabled, evaluate-only or mismatched
policy blocks release publication. Main-only keyless candidate rehearsals remain
available without enabling releases or creating tags.

GitHub intentionally hides bypass lists from callers without ruleset write
access. The read-only Actions check reports that limit and checks bypasses when
visible; it does not claim to inspect hidden lists. The owner verifier checks
every bypass exactly, including the empty immutable-policy list. No administrator
credential is stored in Actions to overcome that visibility boundary.

Verification reads the active REST tag targets and exact full-ref conditions.
The documented per-ref REST rules query is for branches, so it is not presented
as a tag push simulation. No destructive push/deletion canary or actual tag-triggered
`GITHUB_REF_PROTECTED` observation was performed. GitHub documents that variable
as true when rulesets/protections apply to the triggering ref; that flag alone
does not establish the strength of the rules, which is why the independent
policy check is required. The tag rules do not replace application tests,
signature verification or review of the intended release commit.

## Official references

- [GitHub ruleset REST API and permission/actor schema](https://docs.github.com/en/rest/repos/rules).
- [Creation, update and deletion rule semantics](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets).
- [Ruleset creation and supported public-repository plans](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository).
- [GitHub Actions ref protection variables](https://docs.github.com/en/actions/reference/workflows-and-actions/variables).
