# English conversion and preserved technical data

This edition uses English for interface copy, code comments, developer diagnostics,
documentation and code-owned agent instructions. English is the only application
catalog. Historical language-specific Markdown filenames remain so existing links
continue to resolve; their narrative is English. In particular, CHANGELOG.zh.md is
an English translation of the distinct upstream release history, retaining its
versions, dates and contributor links rather than substituting the shorter fork
changelog. Historical release notes describe past behavior, not current defaults.

An English-only application still accepts and analyzes international data. The
following original-language values intentionally retain their bytes:

- Legacy mention tokens and their parser fixtures in chat-mentions. Changing a
  token would break stored conversations and backend protocol compatibility.
- Built-in rule names, tool descriptions, report headings and reporter seeds used
  as exact lookup/migration inputs. The UI resolves recognized legacy values to
  English; user-authored edits remain unchanged. These dictionaries are data, not
  active non-English message catalogs.
- Historical model-review prefixes and approval-summary matching patterns. They
  classify/display older audit records without rewriting their evidence.
- Original detection markers in Sigma, source checks, host triage and positive
  event fixtures. Current emissions use the English marker; old logs must remain
  detectable too.
- Target-language response signatures in the API-recon helper and reference
  search commands. Translating these regex inputs would stop matching the target.
- Accepted input syntax such as the ICP marker and full-width separators. These
  are parser behavior, not product narrative.
- Unicode test inputs that verify composition, escaping, character budgets and
  evidence preservation. Test descriptions and assertions use English while their
  input data can contain any language.
- Recorded side-question validation conversations and an explicitly quoted page
  title in the validation guide. They are historical evidence and are not rewritten
  to make a text scan clean. Their inclusion does not claim a new live validation.

A CJK inventory is therefore a review queue, not proof that every match is a missed
translation. Review comments/narrative separately from exact data. Do not delete
fixtures or weaken compatibility tests merely to reach a zero-character count.
Prompt translation may change model behavior; passing unit/build checks does not
establish identical performance without measured benchmarks.
