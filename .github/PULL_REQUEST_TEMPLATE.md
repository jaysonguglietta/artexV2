## Problem and resulting behavior

Explain the concrete problem and what users experience after this change.

## Validation

List checks actually run, failures/skips and environment limitations. Use isolated
PostgreSQL databases for integration tests; never point tests at production data.

## Security and compatibility

- [ ] Input validation, authorization and credential handling remain enforced.
- [ ] Stored IDs/protocol tokens and original evidence retain compatibility.
- [ ] Destructive or execution-enabling changes are explicitly documented.
- [ ] No credentials, runtime evidence, private logs or generated data are included.
- [ ] Narrative documentation and interface copy are English. Prompt changes identify
      behavior differences and include regression checks; no benchmark equivalence
      is claimed without measurement.

## Deployment or migration notes

Describe required configuration changes, data migration and rollback, if any.
