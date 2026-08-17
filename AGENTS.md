# Repository Guidance

These instructions apply to the entire repository.

## API contracts

- JSON collection fields must be encoded as arrays. Return `[]`, not `null`, when a collection has no values unless the API explicitly defines a semantic difference between absent and empty.
- Keep `null` for genuinely optional single values, such as an active Claim, completion timestamp, parent Task, or mode-specific context.
- Normalize domain values at the application or transport boundary. Do not spread defensive nil handling across UI components.
- When changing an API response shape, update the corresponding frontend type and add a regression test for its empty representation.

## Verification

- Run `gofmt` on changed Go files.
- Run `go test ./...` for backend or shared-contract changes.
- Run `npm run build` and `npm run lint` in `web/` for frontend changes.
- Run `git diff --check` before handing off changes.
