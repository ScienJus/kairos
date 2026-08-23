export type WorkItemStatus = 'open' | 'completed' | 'cancelled' | 'failed' | 'awaiting_agent_acceptance' | 'awaiting_human_acceptance'
export type TaskStatus = 'pending' | 'working' | 'waiting_children' | 'in_review' | 'completed' | 'skipped' | 'failed'
export type Mode = 'blackboard' | 'workflow'

export interface DefinitionBinding { ID: string; Version: number; Mode: Mode }
export interface WorkItem {
  ID: string; Definition: DefinitionBinding; Status: WorkItemStatus; AcceptanceMode?: 'none' | 'agent' | 'human'; Title: string; Goal: string
  Context: string; Constraints: string; AcceptanceCriteria: string; Tags: string[]; Result: string
  Version: number; CreatedAt: string; UpdatedAt: string; CompletedAt: string | null
}
export interface Review {
  ID: string; TaskID: string; SubmissionID: string | null; Status: 'pending' | 'approved' | 'rejected'
  RequestedBy: string; RequestedAt: string; DecidedBy: string | null; DecidedAt: string | null; Feedback: string
}
export interface Submission { ID: string; TaskID: string; ClaimID: string; Result: string; SubmittedAt: string }
export interface ArtifactDefinition { Name: string; Description: string }
export interface Artifact {
  ID: string; WorkItemID: string; TaskID: string; ClaimID: string; SubmissionID: string | null
  Name: string; URI: string; CreatedAt: string
}
export interface Failure { ID: string; TaskID: string; ClaimID: string; Action: string; Reason: string; RetryPrompt: string; FailedAt: string }
export interface Task {
  ID: string; WorkItemID: string; Status: TaskStatus; ActiveClaimID: string | null; ParentTaskID: string | null
  WorkflowTaskID?: string | null
  Title: string; Description: string; AcceptanceCriteria: string; Executor: 'agent' | 'human' | 'either'
  SkippedBy?: ActorRef | null; SkipReason?: string
  AllowedRoles: string[]; Tags: string[]; Reviews: Review[]; Submissions: Submission[]; Failures: Failure[]; TransitionDecisions: unknown[]
  Position: number; CreatedAt: string; UpdatedAt: string; CompletedAt: string | null
  ReviewPolicy?: 'none' | 'executor_decides' | 'required' | null
}
export interface ActorRef { Kind: 'human' | 'agent'; ID: string }
export interface Claim { ID: string; TaskID: string; Executor: ActorRef; ClaimedAt: string; EndedAt: string | null; EndReason: string }
export interface WorkflowChoiceOption {
  ID: string; Kind: 'continue' | 'exit'
  Targets: Array<{ ID: string; Title: string }>
  Relations: Array<{ RelationID: string; Target: { ID: string; Title: string }; Label: string; AgentGuidance: string }>
  SkippableOptionalTasks: Array<{ ID: string; Title: string }>
}
export interface TaskExecutionContext {
  WorkItem: WorkItem; Task: Task; Claims: Claim[]; Artifacts: Artifact[]; ExpectedArtifacts: ArtifactDefinition[]
  Responsibility?: { Kind: string; Actor: ActorRef | null }; Outcome?: { Kind: string; Actor: ActorRef | null; Reason?: string; OccurredAt?: string }
  Workflow: { UpstreamTasks: Task[]; ChoiceGroups: WorkflowChoiceOption[] } | null
  Blackboard: { Tasks: Task[]; Relations: TaskRelation[]; CanDecompose: boolean } | null
}
export interface TaskCapabilities { CanClaim: boolean; CanSubmit: boolean; CanRelease: boolean; CanFail: boolean; CanReview: boolean; CanSkip: boolean; CanDecompose: boolean; CanAddChild: boolean }
export interface TaskDetailView {
  Task: Task; Responsibility: { Kind: string; Actor: ActorRef | null }; Outcome: { Kind: string; Actor: ActorRef | null; Reason?: string; OccurredAt?: string }
  CurrentReview: Review | null; History: { Claims: Claim[]; Submissions: Submission[]; Reviews: Review[]; Failures: Failure[]; TransitionDecisions: unknown[] }; Artifacts: Artifact[]; Capabilities: TaskCapabilities
}
export interface TaskRelation { WorkItemID: string; FromTaskID: string; ToTaskID: string }
export interface BlackboardTaskDecomposition { Parent: Task; Children: Task[] }
export interface DefinitionContext { Name: string; Description: string; AgentInstructions: string; SuggestedTags: string[] }
export interface WorkItemContext { WorkItem: WorkItem; Definition: DefinitionContext; Tasks: Task[]; Relations: TaskRelation[]; Claims: Claim[]; ActiveClaims: Claim[]; Artifacts: Artifact[] }
export interface Definition {
  ID: string; Version: number; Name: string; Description: string; AgentInstructions: string
  SuggestedTags: string[]; Status: 'draft' | 'published' | 'archived'; Graph?: unknown
}
export interface WorkflowTaskDefinition {
  ID: string; Title: string; Description: string; AcceptanceCriteria: string
  Executor: Task['Executor']; AllowedRoles: string[]; Execution: 'required' | 'optional'
  ReviewPolicy: 'none' | 'executor_decides' | 'required'; DefaultTags: string[]; Artifacts: ArtifactDefinition[]
}
export interface WorkflowRelationDefinition { ID: string; FromTaskID: string; ToTaskID: string; Label?: string; AgentGuidance?: string }
export interface WorkflowDefinition extends Definition {
  Graph: { StartTaskIDs: string[]; Tasks: WorkflowTaskDefinition[]; Relations: WorkflowRelationDefinition[]; MaxTaskExecutions: number }
}
export interface Identity { id: string; kind: 'human'; role: '' }
export interface HumanAttentionItem { Kind: 'review' | 'human_task' | 'work_item_acceptance'; WorkItem: WorkItem; Task: Task | null }

export interface TaskDraftInput {
  title: string; description: string; acceptance_criteria: string
  executor: Task['Executor']; allowed_roles: string[]; tags: string[]
}
export interface SubmitTaskInput {
  claim_id: string; result: string; artifact_ids: string[]; request_review: boolean
  transition: { choice_group_id: string; skip_optional_task_ids: string[]; review_skipped_task_ids: string[]; reason: string } | null
}
export interface FailTaskInput { claim_id: string; action: 'reopen' | 'fail_work_item'; reason: string; retry_prompt: string }
export interface DecomposeTaskInput { claim_id: string; children: TaskDraftInput[] }
export interface ReviewDecisionInput { decision: 'approved' | 'rejected'; feedback: string }
export interface CreateDefinitionInput {
  id: string; version: number; name: string; description: string
  agent_instructions: string; suggested_tags: string[]; status: 'draft' | 'published' | 'archived'
}
export interface CreateWorkflowDefinitionInput extends CreateDefinitionInput {
  graph: {
    start_task_ids: string[]
    tasks: Array<{
      id: string; title: string; description: string; acceptance_criteria: string
      executor: Task['Executor']; allowed_roles: string[]; execution: 'required' | 'optional'
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
