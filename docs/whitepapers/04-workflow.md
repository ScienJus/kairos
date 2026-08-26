# Kairos Workflow Mode

> Constraints and executor autonomy in a predefined Task Graph

## Abstract

Workflow organizes a WorkItem with a versioned formal definition. The WorkItem is bound to a fixed Workflow ID and Version at creation. During execution, the system creates Tasks on demand and advances them according to formal relations.

Workflow can also reserve explicit decision space for executors. Each Task can configure its executor type and, for Agent execution, allowed roles, whether it may be skipped, and whether human Review is required. An executor makes progression decisions while completing the current Task. When the executor is an agent, deciding about optional Tasks does not require an extra agent invocation.

## 1. Workflow Structure

A WorkItem is bound to the latest stored Workflow Definition ID and Version when created. This binding remains unchanged throughout the WorkItem lifecycle; later Workflow versions do not alter work that has already started.

A Workflow Definition can also provide Agent Instructions and Suggested Tags that apply to all runtime Tasks. Executors use Suggested Tags to label concrete Tasks dynamically. The tags do not participate in prerequisite or candidate eligibility calculations.

A Workflow Graph consists of start points, Task Definitions, directed Relations, and `MaxTaskExecutions`. A Workflow can have multiple start points. All start Tasks are created with the WorkItem and must therefore be required. A Task Definition can include Default Tags, which the system copies to runtime Tasks; executors can still adjust them as needed.

A Relation may provide an optional `Label` and `AgentGuidance`. `Label` is concise handoff text shown on the graph. `AgentGuidance` enters the current Task's Workflow execution context and helps the executor make existing optional, continue, or exit decisions. Both may be blank, especially on a simple single path with no judgment to exercise. Guidance only explains progression already permitted by the compiled graph; it does not turn a regular Relation into a conditional branch or change required, optional, parallel, or cycle semantics.

The Workflow Definition describes revisitable task nodes and progression relations. Runtime execution starts from the definition’s start points and creates concrete Tasks as nodes are reached:

```text
Workflow Definition: Design → Implement → Test

WorkItem Runtime: Design #1 → Implement #1 → Test #1
```

Every Task instance has independent Claim and result history. Its lifecycle contributes to WorkItem progress. The system creates downstream Tasks after the previous set of Tasks formally ends. When one runtime Task instance has multiple predecessor instances, it waits for all of those concrete instances to end by default.

Each Task Definition may also declare named Artifact delivery instructions. Every declared name is required in a successful runtime Submission. The contract guides the executor with a description but does not prescribe file types or storage; extra Artifacts remain allowed.

The operations UI projects the immutable Definition graph together with runtime Tasks and Relations. Definition nodes that have not produced a runtime Task are shown as `not reached`; they are display-only and cannot be claimed or opened as Task execution context. This complete-graph view does not pre-create Tasks or change Workflow activation and transition semantics. Cycle relations remain visible as return edges, while multiple runtime executions of a cyclic Definition node are summarized with an execution count on that node. Selecting the node opens its latest runtime Task by default; previous and next controls move through its concrete Task instances one at a time, using the normal Task detail view for each instance.

Kairos uses an internal Workflow Task Activation to aggregate predecessor results from one unfolding. Activations use correlation to distinguish parallel branches and different cycle iterations. An Activation creates an executable Task only after every input is resolved; it is never exposed to or claimed by an executor.

A Workflow Definition can contain cycles:

```text
Implement → Test
    ↑        │
    └────────┘
```

Revisiting the same definition node creates a new Task instance:

```text
Implement #1 → Test #1 → Implement #2 → Test #2
```

When a Workflow version is created, the system derives progression choices for each Task from the graph structure. Every outgoing edge that remains inside the current cycle forms its own Continue Group. Edges leaving the cycle are combined into one Exit Group. Multiple Continue Groups and the Exit Group are mutually exclusive, and the executor selects one group. Cycle-internal edges do not express parallelism or prerequisites. A regular acyclic node has only one Exit Group.

Selecting a Continue Group retains and creates its target Task. The target Task’s optional configuration does not apply to that Activation. Unselected Continue Groups do not create Tasks.

After selecting the Exit Group, its required Tasks are created automatically, while the executor still decides whether to retain optional Tasks. Every cycle must have an exit. A Workflow Definition may contain at most 100 Task Definitions and 1,000 Relation Definitions. Start Task IDs must be unique, refer to required Tasks in the graph, and are therefore bounded by the Task Definition limit. `MaxTaskExecutions` limits the total Task instances a WorkItem may create, defaults to 100 when configured as zero, and may not exceed 500. These values serve as runaway protection; zero does not mean unlimited. The runtime Task Graph connects only concrete instances and therefore remains an acyclic execution history.

When an executor submits a Task, Kairos stores a Transition Decision containing the chosen Group, triggered or skipped Relations, executor, and reason. If Review is required, the Decision remains unapplied until Review approves it and downstream Tasks are created. A rejected Decision remains as unapplied history. At most one Decision can be applied for a runtime Task. The Decision, Activation, downstream Tasks, and Task Relations are updated in one transaction.

Parallel prerequisites are still aggregated by concrete instance:

```text
Frontend implementation ─┐
Backend implementation  ─┼→ Integration test
Documentation           ─┘
```

A Task prerequisite has one consistent meaning: the current Task can advance only after all predecessor Tasks are completed or skipped.

## 2. Task Configuration

Workflow configures four settings for each Task:

```text
executor:
  agent
  human
  either

roles:
  - backend

execution:
  required
  optional

review:
  none
  executor_decides
  required
```

`executor` defines whether a Task can be executed by an agent, a person, or either.

`roles` limits the agent roles that can discover and claim the Task. Human Tasks are unaffected by agent roles.

`execution` determines whether a Task may be skipped:

| Value | Semantics |
| --- | --- |
| `required` | The Task must execute |
| `optional` | An executor can retain or skip the Task |

Optional configuration applies to Tasks in an Exit Group. When a Task is selected through a Continue Group, the selection itself is the keep decision and the Task is created directly.

A start Task without predecessors must be `required`. Every optional Task has at least one predecessor, whose executor decides whether it should execute.

`review` defines the human Review requirement before the Task ends:

| Value | Semantics |
| --- | --- |
| `none` | No human Review |
| `executor_decides` | The executor decides whether to request human Review |
| `required` | Human Review must approve |

These settings define where executors may exercise judgment while keeping the overall Workflow structure stable.

## 3. Candidate Tasks

A required Task enters the candidate set when:

```text
all predecessor Tasks are completed or skipped
+ the current Task has not ended
+ the current Task has no Claim
+ executor type matches
+ role matches when the executor is an agent
```

When multiple Tasks meet these conditions, the system returns multiple candidates. A person or agent can choose proactively, and a future Bridge can automate the same role-aware choice.

```text
[Frontend implementation, Backend implementation, Documentation]
```

Unselected required Tasks remain in the candidate set until executed. An executor can also decide to skip an optional Task.

## 4. Optional Task Progression

Whenever a predecessor Task ends, it carries a decision for each connected optional Task. The executor that completed or skipped the predecessor supplies the decision. An agent executor does so without an additional agent invocation.

After every predecessor of an optional Task has ended, the system aggregates their decisions:

- if any executor chooses to retain it, the Task enters the candidate set;
- if every executor chooses to skip it, a skip decision is formed;
- if an executor provides no decision, the Task is retained by default.

```text
Frontend executor: skip ─┐
Backend executor: keep  ─┼→ Documentation enters the candidate set
Design executor: skip   ─┘
```

Skipping requires unanimous agreement:

```text
keep = OR(keep₁, keep₂, ..., keepₙ)
skip = AND(skip₁, skip₂, ..., skipₙ)
```

An optional Task with one predecessor does not wait for other executors. Its decision takes effect as soon as any associated Review requirement is satisfied. When multiple optional Tasks occur consecutively, the same executor can decide all of them in one progression:

```text
Backend implementation
        ↓
Documentation (optional) → skip
        ↓
Update examples (optional) → skip
        ↓
Integration test (required) → enters candidate set
```

The executor stores these decisions as Skip Intent with the current submission and lists only the optional Tasks that this progression may skip. Kairos applies the intent to the currently reachable path according to the Workflow Definition and stops when it reaches a Task that must execute. Parallel paths advance independently; when paths join, the unanimous-agreement rule applies. If any path requires Review, that path waits for approval before advancing.

## 5. Review

When submitting the current Task, the executor follows its Review configuration:

```text
Submit Task
    ↓
Review Policy
 ├── none ───────────→ end
 ├── executor_decides → end / Review
 └── required ───────→ Review

Review approved ─────→ end
Review rejected ─────→ Pending → claim again
```

Review is a state of the same Task. When the executor submits for Review, the system creates an immutable Task Submission from the current Claim, links the Review to that Submission, ends the Claim, and moves the Task to `InReview`. There is no Active Claim while waiting. The Reviewer acts on the Review record and does not claim the Task.

Every Review request, decision, and feedback record remains on the Task in chronological order as complete Review history. Task context supplies the entire history to executors. Approval formally ends the Task. Rejection returns it to `Pending`, allowing either the original or another executor to create a new Claim and continue with the complete Review history.

Skipping an optional Task is also an ending decision, and the same Review configuration applies:

- `none`: skip directly;
- `executor_decides`: enter Review when any predecessor executor requests human confirmation;
- `required`: skip only after human confirmation.

After approval of the skip decision, the optional Task becomes Skipped. Rejection retains the Task and places it in the candidate set. The Review evaluates the predecessor executor’s skip decision and does not create a Claim for the optional Task.

Each executor submits its decision about an optional Task with the current Task. That decision joins aggregation after the current Task’s own Review requirements are satisfied. Once any Review required by the skip decision approves, Workflow continues. An agent executor is not started a second time.

## 6. Executor Autonomy

Workflow uses configuration to define explicit spaces for executor judgment:

```text
People define Task Graph and policies
                ↓
System enforces prerequisites
                ↓
Executor decides optional Tasks and Review
```

Executor autonomy includes:

- choosing work from multiple candidate Tasks;
- deciding whether optional Tasks connected to the current Task are worthwhile;
- deciding whether to request human Review under `executor_decides`.

Workflow continues enforcing prerequisites, required Tasks, and required Review. When the executor is an agent, these decisions express agent autonomy.

## 7. WorkItem Completion

A Task has two ending outcomes:

```text
Completed
Skipped
```

A result requiring Review does not take effect until approval. A WorkItem completes when every produced Task is completed or skipped and Workflow has no downstream Task left to create:

```text
∀ Runtime Task: Completed or Skipped
+ No Next Task
            ↓
    WorkItem Completed
```

When the Task instance count reaches `MaxTaskExecutions`, the WorkItem becomes Failed.

> Workflow defines the constraints; executor autonomy operates at explicitly configured decision points.
