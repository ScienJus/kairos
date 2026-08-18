# Repository Guidance

These instructions apply to the entire repository.

## API contracts

- JSON collection fields must be encoded as arrays. Return `[]`, not `null`, when a collection has no values unless the API explicitly defines a semantic difference between absent and empty.
- Keep `null` for genuinely optional single values, such as an active Claim, completion timestamp, parent Task, or mode-specific context.
- Normalize domain values at the application or transport boundary. Do not spread defensive nil handling across UI components.
- When changing an API response shape, update the corresponding frontend type and add a regression test for its empty representation.

## Documentation

- Treat documentation as part of the change. When behavior, API routes, MCP tools, configuration, schema semantics, UI capabilities, or project status changes, update the corresponding README, whitepaper, handbook, Skill, and examples in the same change.
- Before handing off a feature, search the documentation for old tool counts, obsolete status lists, renamed configuration, and superseded lifecycle descriptions in both English and Chinese.
- Keep current implementation status separate from planned product behavior. Do not describe an implemented capability as planned, or a partially implemented surface as complete.

## Persistence and schema

- Keep the relational schema straightforward and explicit. Fields used for filtering, ordering, indexing, uniqueness, ownership, lifecycle decisions, authorization, or database constraints must be stored in dedicated columns.
- Do not query common domain fields by extracting them from JSON payloads. JSON payloads are appropriate for nested details, immutable snapshots, or data that is read and written only as a complete aggregate.
- When a field begins to participate in database behavior, promote it to a typed column and keep the repository responsible for consistent serialization. Prefer a small amount of intentional duplication over making JSON the effective database schema.
- Add indexes only for concrete query paths. Avoid speculative tables, columns, indexes, triggers, and cross-field constraints that make migrations harder without protecting a current invariant.
- Keep migrations easy to inspect and safe to apply. Do not rewrite migrations that may already be deployed unless the project explicitly treats them as unreleased and the affected development databases will be repaired manually.

## Verification

- Run `gofmt` on changed Go files.
- Run `go test ./...` for backend or shared-contract changes.
- Run `npm run build` and `npm run lint` in `web/` for frontend changes.
- Run `git diff --check` before handing off changes.
