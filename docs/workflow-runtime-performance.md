# Workflow expansion measurements

The per-node execution limit permits much more than 500 Tasks in one WorkItem.
Expansion must therefore avoid repeatedly decoding every historical aggregate for
individual edges. This optimization does not add a total WorkItem Task limit.

## Reproduction

```sh
go test ./internal/repository -run '^$' -bench BenchmarkWorkflowFanoutHistory -benchtime=3x -count=1
```

The benchmark uses a temporary SQLite file and a validated 100-node Definition.
One claimed Task submits a decision that creates eight required successor Tasks.
The setup seeds 100, 1,000 or 10,000 Task/Activation aggregates, spread across nodes
below the 500-instance per-node limit. Beyond the 92 initial Tasks, completed
history contains one 1 KiB Submission result per Task. These are stored aggregate
fixtures, not a measurement of running thousands of agents. Setup is excluded;
each measured submission transaction rolls back so samples retain the same state.

Measured on 2026-09-14, Apple M4 Pro, darwin/arm64, three samples per size:

| Existing Tasks | Before | After | Allocated before | Allocated after |
| --- | ---: | ---: | ---: | ---: |
| 100 | 11.05 ms/op | 3.88 ms/op | 8.21 MB/op | 3.52 MB/op |
| 1,000 | 108.62 ms/op | 15.45 ms/op | 87.09 MB/op | 12.63 MB/op |
| 10,000 | 1,150.77 ms/op | 127.97 ms/op | 967.19 MB/op | 114.01 MB/op |

These are local microbenchmark results, not latency targets or PostgreSQL results.
MB denotes decimal bytes allocated per operation, not peak resident memory.

## Query and complexity changes

Before, a fanout of `F` edges that materialized Tasks used `F + 1` full Activation
list reads and `F + 1` full Task list reads. For eight edges, each list was read
nine times. The repeated decoding/scanning cost was proportional to
`F × (activation history + Task history)`.

Now each edge looks up its waiting activation by WorkItem/node/correlation/status,
counts resolved activations for its target node, and queries the next position
with `MAX(position) + 1`. Existing typed columns and indexes support these reads;
no JSON field extraction, new migration or cross-request cache is involved.
Duplicate waiting activations still produce a conflict, and the existing
WorkItem write transaction serializes counting and creation. The count includes
start, skipped and retry activations, and excludes waiting activations.

The measured fanout now performs one full Activation list read for decision
receipts and one full Task list read for completion. The edge queries read only
the waiting aggregate or scalar values. Total SQL query count is not reduced:
each materializing edge now performs three scoped queries instead of two full
list queries. The improvement comes from reading and decoding less data.

History cost has not disappeared: receipt construction and completion still
scale with history, and recursively applied skip decisions each read receipts.
Claim discovery, context responses and recovery routing have separate costs.
This change removes the measured per-edge amplification; it does not establish
capacity at 50,000 Tasks or optimize every execution path.

`TestWorkflowFanoutDoesNotReloadHistoryPerEdge` checks the full-history read budget
and all eight persisted successor positions. `TestWorkflowRuntimeQueriesScopeAndWaitingConflict`
checks lookup scope, waiting exclusion from counts, sparse/empty positions and
duplicate-waiting detection. Existing join, skip, retry, limit and recovery tests
cover transaction behavior through the same expansion path.
