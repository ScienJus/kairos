# Kairos Coordination Semantics

> Why the same Task Graph produces different next steps in Workflow and Blackboard

## Abstract

Workflow and Blackboard store work with the same WorkItem, Task, and Task Relation objects. What changes is how much authority the graph has when Kairos decides which Tasks can be worked on now.

In Workflow, the graph is a set of rules: unmet dependencies block a Task. In Blackboard, the graph records the team's current recommendation: it guides the next choice without turning every relation into a gate. In both modes, people and agents still choose a specific Task from the available candidates.

## 1. Unified Form

The work structure inside one WorkItem can be expressed as:

```text
G = (T, R)

T = set of Tasks
R = set of Task Relations
```

The coordination mode produces a candidate set from the Task Graph and current context:

```text
Candidates = Coordination(mode, G, context)
```

The candidate set defines only the current choice space. A person or agent can choose proactively, and a future Bridge can automate the same role-aware choice. A Claim then establishes unique execution responsibility for one concrete actor.

## 2. Workflow Candidate Semantics

Workflow treats Task Relations as formal constraints. A Task enters the candidate set only when its prerequisites are satisfied:

```text
Cworkflow(actor) = {
  t ∈ T |
  unfinished(t)
  ∧ unclaimed(t)
  ∧ prerequisites_satisfied(t)
  ∧ (required(t) ∨ kept(t))
  ∧ executor_matched(t, actor)
  ∧ role_matched(t, actor)
}
```

`kept(t)` means that upstream executors decided to retain an optional Task. Role constraints apply only to agent executors.

For example:

```text
Design ──→ Implement ──→ Test
```

After “Design” completes, “Implement” enters the candidate set. After “Implement” completes, “Test” enters it. One Task connected to multiple downstream Tasks can produce several candidates at once.

Workflow guarantees that the candidate set complies with the formal plan. Executors continue making choices within that legal space.

## 3. Blackboard Candidate Semantics

Blackboard treats Task Relations as shared progression guidance. Candidates primarily come from unfinished, unclaimed Tasks that match the current query context:

```text
Cblackboard = {
  t ∈ T |
  unfinished(t)
  ∧ unclaimed(t)
  ∧ matches(t, context)
}
```

For example:

```text
Design ⇢ Implement ⇢ Test
```

“Implement” can remain a candidate before “Design” completes, while carrying the guidance that it should wait for Design. The executor decides using the WorkItem objective, existing results, suggested relations, and its own context.

Blackboard uses shared information to help executors understand the choice space, and the Task Graph continues evolving through those decisions.

To keep one collaboration space operationally bounded, a Blackboard WorkItem accepts at most 1,000 Task instances and 10,000 suggested Relations. These are hard safety ceilings covering completed history and decomposition children; exceeding either ceiling rejects the write.

## 4. Graph Authority and Evolution

| Dimension | Workflow | Blackboard |
| --- | --- | --- |
| Role of graph | Formal plan | Current shared judgment |
| Relation semantics | Execution constraint | Progression guidance |
| How Tasks are created | From the process definition | Dynamically by collaborators |
| Structural changes | Follow formal rules | Follow the evolving understanding of work |
| System responsibility | Execute the plan and limit legal candidates | Persist the plan and provide shared state |

A typical Workflow proceeds as:

```text
Bind Workflow Version → create Tasks on demand → execute → advance → complete
```

A Workflow Definition can contain cycles. Every revisit to a definition node creates a new Task instance, so the runtime Task Graph remains an actual execution history. A progression choice continues or exits a cycle. A maximum Task instance count is only a runaway safeguard; exceeding it fails the WorkItem.

Blackboard forms a continuous feedback loop:

```text
plan → execute → observe → adjust plan
 ↑                              ↓
 └──────────────────────────────┘
```

Blackboard is therefore dynamic throughout execution. Collaborators complete Tasks while continuously improving the structured understanding of the WorkItem.

## 5. Completion Semantics

Workflow completion is derived from formal graph closure:

```text
Every produced Workflow Task is Completed or Skipped
+ no structural progression remains pending
                         ↓
                 WorkItem Completed
```

In Workflow, structural progression is determined by the Definition, path choices, and cycle state. Blackboard has no formal graph closure: when its current Tasks converge, the WorkItem remains open and exposes a completion decision. A collaborator either creates more Tasks or explicitly submits a durable completion result. Only that submission applies the configured acceptance policy and can eventually complete the WorkItem.

The coordination mode therefore determines both how Tasks are produced and how WorkItem completion is declared.

## 6. Mode Boundary

A WorkItem uses one Coordination Mode at a time, and that mode interprets every Task Relation consistently:

```text
Workflow   → Relation is a formal constraint
Blackboard → Relation is progression guidance
```

Consistent interpretation keeps the two modes clear and avoids configuring an independent coordination strategy on every relation in the same underlying Task Graph.

The choice between Workflow and Blackboard depends on the work:

- use Workflow when the system must guarantee work order;
- use Blackboard when collaborators must continuously discover and adjust the plan during execution.

## 7. Core Conclusions

The Kairos coordination semantics can be summarized as:

1. Workflow and Blackboard share the same Task Graph structure.
2. Workflow computes legal candidate Tasks with authoritative constraints.
3. Blackboard uses shared structure and context to provide candidates and progression guidance.
4. Both modes preserve the executor’s choice of a specific Task.
5. Every Task Relation in one WorkItem follows the semantics of that WorkItem’s mode.

> Workflow uses the graph to enforce the path. Blackboard uses it to share the team's current best plan.
