# Kairos Core Work Model

> Definitions of WorkItem, Task, Workflow, and Blackboard, and how they map to one unified model

## Abstract

Kairos uses `WorkItem` to represent a complete work objective and `Task` to represent a unit of work that a person or agent can execute and deliver. The Tasks and relations within one WorkItem form a Task Graph.

Workflow and Blackboard are two organizational semantics for that Task Graph. Workflow uses a formal, authoritative plan. Blackboard allows collaborators to continuously form and adjust the plan during execution. Both modes share the same underlying work model.

## 1. WorkItem and Task

### 1.1 WorkItem

A `WorkItem` represents a complete work objective: the final outcome expected by a person or system.

Examples:

```text
Implement login
Fix duplicate charges in the payment system
Complete the first conceptual design of Kairos
```

A WorkItem contains:

- the work objective;
- background and context;
- constraints and acceptance criteria;
- final deliverables;
- its internal Tasks and their relations.

A WorkItem can be created with a complete plan, or with only an objective so that collaborators form the plan during execution.

Every WorkItem is bound at creation to a fixed version of a Coordination Definition. The Definition selects Workflow or Blackboard and provides the collaboration space name, description, Agent Instructions, and Suggested Tags. A Workflow Definition additionally defines the formal execution structure; a Blackboard Definition does not predefine a Task Graph.

> A WorkItem is the boundary of an objective and its outcome.

### 1.2 Task

A `Task` is a unit of work decomposed from a WorkItem that a person or agent can execute and deliver.

```text
WorkItem: Implement login
├── Task: Design the login approach
├── Task: Implement the login API
└── Task: Test login
```

A Task is the execution boundary of one executor:

- it can appear as an independent work candidate;
- it has explicit execution content and a deliverable result;
- it has only one responsible executor while being executed;
- its progress and results are recorded on the Task and become shared WorkItem context.

A Task should be small enough for one executor to own through one coherent work session and produce a deliverable. It can be restricted to an agent, a person, or either.

In Blackboard, an executor can also decompose a Task into child Tasks before producing a result. The parent becomes an aggregation boundary and no longer receives a result directly; it completes after all child Tasks end. A Task uses exactly one delivery style: direct delivery or child Task aggregation.

Whenever an executor formally submits a result, Kairos creates an immutable Task Submission under the Task and links it to the Claim that produced the result. A Task can go through multiple execution, submission, and Review rounds. Every Submission remains in shared history.

When an executor reports failure, Kairos creates an immutable Task Failure under the Task. A prompt supplied when reopening becomes part of the next execution context; a global failure ends both the Task and WorkItem. Claims, Submissions, Reviews, Failures, and progression decisions also form an append-only WorkItem Event history.

> A WorkItem answers “What final outcome is required?” A Task answers “What concrete work comes next?”

## 2. Task Graph

A WorkItem contains zero or more Tasks. Directed relations between Tasks form a Task Graph.

```text
Design login → Implement login API → Test login
```

A Task Graph can express:

- Task decomposition hierarchy;
- prerequisites;
- parallel work;
- one Task connected to multiple downstream Tasks;
- multiple Tasks connected to one downstream Task;
- decomposition and aggregation of work.

Workflow and Blackboard use the same runtime Task Graph. Their organizational semantics determine how the graph is produced, how it evolves, and whether a relation constrains execution. Workflow additionally uses a versioned formal definition to determine how the runtime graph unfolds.

## 3. Workflow

`Workflow` organizes the work inside a WorkItem with a versioned formal definition. A WorkItem is bound to a published Workflow Definition ID and Version when created and is unaffected by later Workflow versions.

```text
Design ──→ Implement ──→ Test
```

Relations in the definition are authoritative constraints. “Design → Implement” means that the system creates the “Implement” Task for this WorkItem only after “Design” has completed.

Workflow instantiates Tasks as progression requires. When execution reaches the same definition node more than once, it creates a new Task instance each time, preserving an independent Claim, progress, and result for every pass. The resulting runtime Task Graph records the actual execution history.

Key characteristics of Workflow include:

- the WorkItem is bound to a fixed Workflow Definition ID and Version;
- Tasks and relations come from that formal version;
- Task instances are created on demand as the Workflow advances;
- the system enforces prerequisites;
- structural changes during execution are constrained by formal rules;
- the system computes the currently legal candidate Tasks from the structure;
- WorkItem completion can usually be derived from the formal structure.

A Workflow can expose multiple legal candidate Tasks at once. Workflow limits the choice space; a person or agent can choose proactively, or a Bridge can dispatch a Task.

> Workflow is a formally defined and authoritative Task Graph.

## 4. Blackboard

`Blackboard` is an open collaboration space maintained collectively inside a WorkItem. The WorkItem supplies the objective, background, constraints, and acceptance criteria, while the Task Graph emerges during execution.

The WorkItem is bound to a fixed Blackboard Definition version. The Definition identifies the collaboration space and provides global instructions, Agent Instructions, and Suggested Tags, but no initial Task Graph.

The initial state can contain only an objective:

```text
WorkItem: Implement login
Tasks: []
```

Collaborators create a plan from their current understanding:

```text
[ ] Design the login approach
[ ] Implement the login API
[ ] Test login
```

The plan can continue evolving as the work becomes better understood:

```text
[x] Design the login approach
[ ] Implement password login
[ ] Implement session management
[ ] Add brute-force protection
[ ] Test login
```

Tasks in Blackboard can also have prerequisite relations:

```text
Design login ⇢ Implement login API ⇢ Test login
```

These relations express the collaborators’ current shared guidance about progression. An executor can use the actual context to start early, work in parallel, adjust relations, or create new Tasks.

Key characteristics of Blackboard include:

- the initial Task Graph can be empty or incomplete;
- collaborators dynamically create, decompose, and adjust Tasks;
- prerequisite relations are guidance by default;
- executors choose the next work from the objective and shared context;
- before ending the current Task, the executor decides whether follow-up Tasks are needed; the WorkItem completes when no unfinished Task remains.

> Blackboard is a Task Graph continuously planned and evolved by collaborators.

## 5. Unified Underlying Model

Workflow and Blackboard map to the same basic structure:

```text
                         WorkItem
                    “Implement login”
                            │
                        Task Graph
                            │
              ┌─────────────┴─────────────┐
              │                           │
           Workflow                   Blackboard
       formal, constrained graph     dynamic, advisory graph
```

The underlying model has three core concepts. Tasks use Parent Task to express hierarchy and Task Relation to express directed relations:

```text
WorkItem
Task
Task Relation
```

The organizational semantics differ as follows:

| Dimension | Workflow | Blackboard |
| --- | --- | --- |
| WorkItem | Complete work objective | Complete work objective |
| Task | Executable, deliverable unit | Executable, deliverable unit |
| Initial Task Graph | Usually predefined | Usually empty or incomplete |
| How Tasks are created | From a formal plan | Dynamically planned by collaborators |
| How the graph evolves | Runs and changes under rules | Evolves continuously with collaboration |
| Task Relation | Execution constraint | Progression guidance |
| Candidate Tasks | Computed from structure | Formed from structure and context |
| Completion | Usually derivable from formal structure | Judged against the WorkItem objective |

Workflow and Blackboard therefore share data structures while applying different coordination semantics:

> A Workflow graph specifies how work advances; a Blackboard graph records how collaborators currently believe work should advance.

## 6. Presentation

The WorkItem collection has List and Kanban views:

```text
WorkItem Collection
    ├── List
    └── Kanban
```

List supports search, filtering, and high-density browsing. Kanban shows the state and flow of complete work. Both views present the same WorkItems.

Inside a WorkItem, Tasks are rendered according to the coordination mode:

```text
WorkItem Detail
    ├── Workflow   → Flow Graph
    └── Blackboard → Checklist
```

Flow Graph presents formal Workflow dependencies and progression state. Checklist presents the dynamically formed Blackboard Tasks, tags, suggested relations, and shared results.

Kanban displays WorkItems only. Tasks remain inside their owning WorkItem and are operated through Flow Graph or Checklist.

## 7. Core Definitions

The Kairos core work model can be summarized as:

```text
WorkItem   = a complete work objective
Task       = an executable, deliverable unit of work
Workflow   = a formally defined and authoritative Task Graph
Blackboard = a Task Graph dynamically planned and evolved by collaborators
```

> Workflow executes an authoritative plan; Blackboard grows a shared plan while executing the work.
