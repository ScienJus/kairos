# Kairos Coordination Semantics

Workflow and Blackboard both show work as a Task Graph, which can make them look like two presentations of the same mechanism. They are not. In Workflow, the graph is an execution rule; in Blackboard, it is the team's current advice to itself.

That difference in authority explains almost everything else: when a Task becomes a candidate, who may change the graph, and what it takes to call the WorkItem complete.

## The Shared Coordination Loop

```text
Task Graph + current context
             ↓
         candidate set
             ↓
       select executor and Claim
             ↓
          submit result
             ↓
      update graph or lifecycle
```

A candidate means only “work that may currently be considered.” It does not assign responsibility or encode priority through list order. A Human, proactive Agent, or Agent Daemon selects a candidate; the Claim then establishes responsibility for one actor.

## How Workflow Produces Candidates

The Workflow Definition Graph is a formal plan. Runtime Task instances are created from prerequisite completion, branch decisions, Reviews, and loop state.

A Task is a candidate only when:

- it has been instantiated from the pinned Definition;
- its state is claimable and it has no active Claim;
- the WorkItem still permits execution;
- executor kind and Agent role match.

Formal Relations may block downstream work; an executor cannot bypass them by judgment. See [Workflow Mode](04-workflow.md) for branch, optional, Review, loop, and recovery semantics.

## How Blackboard Produces Candidates

A Blackboard Task Graph records the current shared plan. Relations provide guidance only. An unfinished predecessor does not automatically block a later Task; the executor considers the Relation, existing results, and current objective.

Suppose “collect evidence” points to “draft report.” In Workflow, the report Task cannot become available until the evidence Task satisfies the graph. In Blackboard, the same arrow warns the report author about missing evidence but does not prevent an informed decision to start drafting. The drawing is similar; the authority is not.

An ordinary Task candidate must:

- be claimable with no active Claim;
- match executor kind, role, and queried tags;
- belong to a WorkItem that still permits execution.

An empty Blackboard, converged Blackboard, or pending Agent acceptance has no particular Task to Claim. It therefore produces a coordination candidate. An Agent first creates a Coordination Claim, then reads full context and creates work, submits completion, or accepts completion.

See [Blackboard Mode](05-blackboard.md) for decomposition, appends, Relations, Skip, and acceptance semantics.

## Graph Evolution: Fixed Rules or an Appendable Plan

| Dimension | Workflow | Blackboard |
| --- | --- | --- |
| Planning authority | Pinned Definition version | Committed Tasks and Relations in the WorkItem |
| Runtime plan changes | Keep the Definition fixed; submit configured decisions | Append Tasks, children, and Relations |
| Relation effect | Constrains progression | Provides guidance |
| History | Retain every runtime Task instance and decision | Retain every committed structure and result |

Both modes append history. Neither rewrites completed facts to express a new plan.

## Reaching Completion

- **Workflow** completes when every instantiated Task on the selected path has ended and no further Task needs to be created.
- **Blackboard** convergence only creates an opportunity to judge completion. A collaborator must submit a durable completion result, after which `acceptance_mode` completes immediately or enters Agent/Human acceptance.

Cancellation and failure are separate terminal paths. Every terminal WorkItem ends active Claims and prevents later Task mutations.

## The Boundary Between the Modes

One WorkItem uses one coordination mode. Workflow does not admit arbitrary new runtime work, and Blackboard does not silently promote advisory Relations into hard dependencies.

The practical distinction is simple: Workflow asks which predefined path is legal now; Blackboard asks which work is still worth doing given what the team currently knows.
