export type WorkItemStatus = 'open' | 'completed' | 'cancelled' | 'failed' | 'awaiting_agent_acceptance' | 'awaiting_human_acceptance'
export type TaskStatus = 'pending' | 'working' | 'waiting_children' | 'in_review' | 'completed' | 'skipped' | 'failed'
export type Mode = 'blackboard' | 'workflow'
export type AuthenticationMode = 'trusted' | 'authenticated'

export interface DefinitionBinding { id: string; version: number; mode: Mode }
export interface WorkItem {
  id: string; definition: DefinitionBinding; status: WorkItemStatus; acceptance_mode: 'none' | 'agent' | 'human'; title: string; goal: string
  context: string; constraints: string; acceptance_criteria: string; tags: string[]; result: string
  version: number; created_at: string; updated_at: string; completed_at: string | null
  cancelled_at: string | null; cancelled_by: ActorRef | null; cancellation_reason: string
}
export interface Review {
  id: string; task_id: string; submission_id: string | null; status: 'pending' | 'approved' | 'rejected'
  requested_by: string; requested_at: string; decided_by: string | null; decided_at: string | null; feedback: string
}
export interface Submission { id: string; task_id: string; claim_id: string; result: string; submitted_at: string }
export interface ArtifactDefinition { name: string; description: string }
export interface Artifact {
  id: string; work_item_id: string; task_id: string; claim_id: string; submission_id: string | null
  name: string; uri: string; created_at: string
}
export interface Failure { id: string; task_id: string; claim_id: string; action: string; reason: string; retry_prompt: string; failed_at: string }
export interface Task {
  id: string; work_item_id: string; status: TaskStatus; active_claim_id: string | null; parent_task_id: string | null
  workflow_task_id: string | null; workflow_activation_id: string | null; decomposed_at: string | null
  title: string; description: string; acceptance_criteria: string; executor: 'agent' | 'human' | 'either'
  skipped_by: ActorRef | null; skip_reason: string
  allowed_roles: string[]; tags: string[]; reviews: Review[]; submissions: Submission[]; failures: Failure[]; transition_decisions: unknown[]
  position: number; created_at: string; updated_at: string; completed_at: string | null
  execution: 'required' | 'optional' | null; review_policy: 'none' | 'executor_decides' | 'required' | null; version: number
}
export interface ActorRef { kind: 'human' | 'agent'; id: string }
export interface Claim {
  id: string; task_id: string; executor: ActorRef; claimed_at: string; last_heartbeat_at: string
  lease_until: string; lease_seconds: number; ended_at: string | null; end_reason: string
}
export interface WorkflowChoiceOption {
  id: string; kind: 'continue' | 'exit'
  targets: Array<{ id: string; title: string }>
  relations: Array<{ relation_id: string; target: { id: string; title: string }; label: string; agent_guidance: string }>
  skippable_optional_tasks: Array<{ id: string; title: string }>
}
export interface TaskExecutionContext {
  work_item: WorkItem; task: Task; claims: Claim[]; artifacts: Artifact[]; expected_artifacts: ArtifactDefinition[]
  responsibility: { kind: string; actor: ActorRef | null }; outcome: { kind: string; actor: ActorRef | null; reason: string; occurred_at: string | null }
  workflow: { upstream_tasks: Task[]; choice_groups: WorkflowChoiceOption[] } | null
  blackboard: { tasks: Task[]; relations: TaskRelation[]; can_decompose: boolean } | null
}
export interface TaskCapabilities { can_claim: boolean; can_submit: boolean; can_release: boolean; can_fail: boolean; can_review: boolean; can_skip: boolean; can_decompose: boolean; can_add_child: boolean }
export interface TaskDetailView {
  task: Task; responsibility: { kind: string; actor: ActorRef | null }; outcome: { kind: string; actor: ActorRef | null; reason: string; occurred_at: string | null }
  current_review: Review | null; history: { claims: Claim[]; submissions: Submission[]; reviews: Review[]; failures: Failure[]; transition_decisions: unknown[] }; artifacts: Artifact[]; capabilities: TaskCapabilities
}
export interface TaskRelation { work_item_id: string; from_task_id: string; to_task_id: string }
export interface BlackboardTaskDecomposition { parent: Task; children: Task[] }
export interface DefinitionContext { name: string; description: string; agent_instructions: string; suggested_tags: string[] }
export interface WorkItemContext { work_item: WorkItem; definition: DefinitionContext; tasks: Task[]; relations: TaskRelation[]; claims: Claim[]; active_claims: Claim[]; artifacts: Artifact[] }
export interface Definition {
  id: string; version: number; name: string; description: string; agent_instructions: string
  suggested_tags: string[]; graph?: unknown
}
export interface WorkflowTaskDefinition {
  id: string; title: string; description: string; acceptance_criteria: string
  executor: Task['executor']; allowed_roles: string[]; execution: 'required' | 'optional'
  review_policy: 'none' | 'executor_decides' | 'required'; default_tags: string[]; artifacts: ArtifactDefinition[]
}
export interface WorkflowRelationDefinition { id: string; from_task_id: string; to_task_id: string; label?: string; agent_guidance?: string }
export interface WorkflowDefinition extends Definition {
  graph: { start_task_ids: string[]; tasks: WorkflowTaskDefinition[]; relations: WorkflowRelationDefinition[]; max_task_executions: number }
}
export interface AuthenticationConfig { mode: AuthenticationMode }
export interface Identity { id: string; kind: 'human' | 'agent'; role: string }
export interface HumanAttentionItem { kind: 'review' | 'human_task' | 'work_item_acceptance'; work_item: WorkItem; task: Task | null }

export interface TaskDraftInput {
  title: string; description: string; acceptance_criteria: string
  executor: Task['executor']; allowed_roles: string[]; tags: string[]
}
export interface SubmitTaskInput {
  claim_id: string; result: string; artifact_ids: string[]; request_review: boolean
  transition: { choice_group_id: string; skip_optional_task_ids: string[]; review_skipped_task_ids: string[]; reason: string } | null
}
export interface FailTaskInput { claim_id: string; action: 'reopen' | 'fail_work_item'; reason: string; retry_prompt: string }
export interface DecomposeTaskInput { claim_id: string; children: TaskDraftInput[] }
export interface ReviewDecisionInput { decision: 'approved' | 'rejected'; feedback: string }
export interface CreateDefinitionInput {
  id: string; base_version?: number; name: string; description: string
  agent_instructions: string; suggested_tags: string[]
}
export interface CreateWorkflowDefinitionInput extends CreateDefinitionInput {
  graph: {
    start_task_ids: string[]
    tasks: Array<{
      id: string; title: string; description: string; acceptance_criteria: string
      executor: Task['executor']; allowed_roles: string[]; execution: 'required' | 'optional'
      review_policy: 'none' | 'executor_decides' | 'required'; default_tags: string[]
      artifacts: Array<{ name: string; description: string }>
    }>
    relations: Array<{ id: string; from_task_id: string; to_task_id: string; label: string; agent_guidance: string }>
    max_task_executions: number
  }
}
export interface CreateWorkItemInput {
  definition_id: string; mode: Mode; title: string; goal: string
  context: string; constraints: string; acceptance_criteria: string; acceptance_mode: 'none' | 'agent' | 'human'; tags: string[]
}
