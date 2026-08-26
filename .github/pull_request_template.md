## Summary

Describe the user-visible or contract-level change and why it is needed.

## Verification

List the commands and manual checks that were run.

## Checklist

- [ ] Tests cover new behavior and important edge cases.
- [ ] Changed Go files were formatted with `gofmt`.
- [ ] `make go-test` passes for backend or shared-contract changes.
- [ ] Frontend tests, lint, and build pass for frontend changes.
- [ ] API collection fields preserve the `[]` rather than `null` contract.
- [ ] Schema and migration effects are documented and tested.
- [ ] English and Chinese documentation reflect behavior and status changes.
- [ ] No credentials, tokens, local data, or generated build output are included.
