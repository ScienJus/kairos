# Repository Guidance

These instructions apply to the entire repository.

## API contracts

- JSON collection fields must be encoded as arrays. Return `[]`, not `null`, when a collection has no values unless the API explicitly defines a semantic difference between absent and empty.
- Keep `null` for genuinely optional single values, such as an active Claim, completion timestamp, parent Task, or mode-specific context.
- Normalize domain values at the application or transport boundary. Do not spread defensive nil handling across UI components.
- When changing an API response shape, update the corresponding frontend type and add a regression test for its empty representation.
- Keep OpenAPI constraints aligned with the server's exact semantics. Do not use character-count constraints to represent UTF-8 byte limits; describe byte limits explicitly with a schema description or extension.
- When request validation changes, inspect every request and response representation of the affected field across HTTP, MCP, frontend types, and generated views.
- When a field has mode-specific semantics, keep the domain model, application behavior, OpenAPI description, documentation, and tests consistent for every mode.

## Change impact

- Before implementing a behavioral or contract change, identify every affected layer: domain validation, application transactions, repository persistence, HTTP and MCP transports, OpenAPI, frontend types, documentation, Skills, and examples.
- Treat changes to lifecycle status, terminal behavior, idempotency, limits, or field semantics as cross-layer changes even when the initial code edit appears local.
- When adding a limit to a stored field, inspect both direct user input and server-generated, summarized, or aggregated values written to the same field.
- After addressing review findings, repeat the full impact scan instead of checking only the most recently reported issue.

## Documentation

- Treat documentation as part of the change. When behavior, API routes, MCP tools, configuration, schema semantics, UI capabilities, or project status changes, update the corresponding README, API reference, OpenAPI document, whitepaper, handbook, frontend type, Skill, and examples in the same change.
- When server errors, terminal states, retry behavior, or recovery behavior changes, update the relevant Agent Skill stop/retry instructions in the same change.
- Before handing off a feature, search the repository for old constants, tool counts, obsolete status lists, renamed configuration, superseded field semantics, and lifecycle descriptions in both English and Chinese.
- Keep current implementation status separate from planned product behavior. Do not describe an implemented capability as planned, or a partially implemented surface as complete.

## Persistence and schema

- Keep the relational schema straightforward and explicit. Fields used for filtering, ordering, indexing, uniqueness, ownership, lifecycle decisions, authorization, or database constraints must be stored in dedicated columns.
- Do not query common domain fields by extracting them from JSON payloads. JSON payloads are appropriate for nested details, immutable snapshots, or data that is read and written only as a complete aggregate.
- When a field begins to participate in database behavior, promote it to a typed column and keep the repository responsible for consistent serialization. Prefer a small amount of intentional duplication over making JSON the effective database schema.
- Add indexes only for concrete query paths. Avoid speculative tables, columns, indexes, triggers, and cross-field constraints that make migrations harder without protecting a current invariant.
- Keep migrations easy to inspect and safe to apply. Do not rewrite migrations that may already be deployed unless the project explicitly treats them as unreleased and the affected development databases will be repaired manually.
- Make operations that intentionally return an error after committing state explicit in application code and documentation. Add a real SQL repository regression test for commit-on-error behavior; an in-memory repository without rollback is not sufficient evidence.
- Any transition to a terminal WorkItem state must consistently end active Claims and prevent subsequent Task mutations.
- Determine whether the affected behavior or schema has been formally released before designing compatibility handling or migrations. State the release assumption when it changes the implementation or review conclusion.

## Verification

- When adding a feature or changing behavior, add corresponding unit tests in the same change. Tests must cover the new contract and its important edge cases.
- Tests for a specific guard or resource limit must assert the guard-specific error or reason, not only a shared final status.
- Build fixtures so an earlier validation or capacity guard cannot fire before the branch under test. Validate manually constructed domain fixtures with the relevant domain validator when practical.
- Cover the exact boundary, one value beyond it, multibyte UTF-8 input, and server-generated or aggregate values when those cases are relevant.
- For transactional side effects, assert both the returned error and the committed repository state. Use a real repository test when rollback or commit behavior is part of the contract.
- Run `gofmt` on changed Go files.
- Run `make go-test` for backend or shared-contract changes.
- Run `npm run build` and `npm run lint` in `web/` for frontend changes.
- Run `git diff --check` before handing off changes.
