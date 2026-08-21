import {
  useEffect,
  useState,
  type FormEvent,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
} from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import {
  Check,
  ChevronDown,
  ChevronRight,
  ChevronsDown,
  ChevronsUp,
  CircleDot,
  Plus,
  ShieldCheck,
  Trash2,
  X,
} from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import {
  canAddBlackboardChild,
  canSkipBlackboardTask,
  refreshTaskState,
  refreshWorkItemState,
} from "./taskOperations";
import type {
  Claim,
  Identity,
  Review,
  ReviewDecisionInput,
  Task,
  TaskDetailView,
  TaskDraftInput,
  TaskExecutionContext,
} from "./types";
import { FormError, Modal, Status, formValue, splitValues } from "./ui";

type TaskDraft = {
  title: string;
  description: string;
  acceptance: string;
  executor: Task["Executor"];
  tags: string;
};
type TaskOperation =
  | "start"
  | "complete"
  | "release"
  | "fail"
  | "review"
  | "decompose"
  | "add-child"
  | "skip";
const emptyTaskDraft = (): TaskDraft => ({
  title: "",
  description: "",
  acceptance: "",
  executor: "either",
  tags: "",
});
function taskDraftInput(draft: TaskDraft) {
  return {
    title: draft.title.trim(),
    description: draft.description.trim(),
    acceptance_criteria: draft.acceptance.trim(),
    executor: draft.executor,
    allowed_roles: [],
    tags: splitValues(draft.tags),
  };
}

function shortID(id: string) {
  return id.slice(0, 8).toUpperCase();
}

const responsibilityLabels = {
  unclaimed: "responsibility",
  claimed_by: "responsibility",
  submitted_by: "submittedBy",
  executed_by: "executedBy",
  decomposed_by: "decomposedBy",
  skipped_by: "skippedBy",
  skip_requested_by: "skipRequestedBy",
  review_requested_by: "reviewRequestedBy",
  failed_by: "failedBy",
} as const;

export function TaskDetail({
  task,
  activeClaim,
  executionClaim,
  identity,
  mode,
}: {
  task: Task;
  activeClaim: Claim | null;
  executionClaim?: Claim | null;
  identity: Identity;
  mode: string;
}) {
  const { t, formatTime } = useI18n();
  const detail = useQuery({
    queryKey: ["task-detail", identity, task.ID],
    queryFn: () => api.getTaskDetail(identity, task.ID),
    retry: false,
  });
  const capabilities = detail.data?.Capabilities;
  const needsExecutionContext =
    capabilities?.CanClaim ||
    capabilities?.CanSubmit ||
    capabilities?.CanRelease ||
    capabilities?.CanFail ||
    capabilities?.CanDecompose;
  const execution = useQuery({
    queryKey: ["task-context", identity, task.ID],
    queryFn: () => api.getTaskContext(identity, task.ID),
    enabled: needsExecutionContext === true,
    retry: false,
  });
  const detailTask = detail.data?.Task ?? task;
  const hasPendingReview =
    capabilities?.CanReview === true &&
    detail.data?.CurrentReview?.Status === "pending";
  const canDecompose = execution.data?.Blackboard?.CanDecompose === true;
  const operationTask = execution.data?.Task ?? detailTask;
  const operationClaim =
    execution.data?.Claims.find(
      (item) =>
        !item.EndedAt &&
        item.Executor.Kind === identity.kind &&
        item.Executor.ID === identity.id,
    ) ?? activeClaim;
  const operationOwnsClaim =
    operationClaim?.Executor.Kind === identity.kind &&
    operationClaim.Executor.ID === identity.id;
  const defaultOperation: TaskOperation | null = hasPendingReview
    ? "review"
    : capabilities?.CanClaim
      ? "start"
      : operationTask.Status === "working" && operationOwnsClaim
        ? canDecompose
          ? "decompose"
          : "complete"
        : capabilities?.CanAddChild
          ? "add-child"
          : capabilities?.CanSkip
            ? "skip"
            : null;
  const [activeOperation, setActiveOperation] = useState<TaskOperation | null>(
    defaultOperation,
  );
  useEffect(() => {
    setActiveOperation(defaultOperation);
  }, [defaultOperation]);
  const latestResult = detail.data?.History.Submissions.at(-1)?.Result;
  const reviewStatus = (status: Review["Status"]) =>
    t(
      status === "approved"
        ? "statusApproved"
        : status === "rejected"
          ? "statusRejected"
          : "statusPending",
    );
  if (detail.isLoading)
    return (
      <div className="panel-placeholder">
        <strong>{t("acquiring")}</strong>
      </div>
    );
  if (detail.error || !detail.data)
    return (
      <div className="error-banner">
        <FormError error={detail.error ?? new Error(t("unreachable"))} />
      </div>
    );
  return (
    <div className="task-detail">
      <div className="task-identity">
        <span>TASK / {shortID(detailTask.ID)}</span>
        <Status value={detailTask.Status} />
        <h3>{detailTask.Title}</h3>
        <p>{detailTask.Description || t("noDescription")}</p>
        {(detailTask.Tags?.length ?? 0) > 0 && (
          <div className="task-tags detail" aria-label={t("tags")}>
            {detailTask.Tags?.map((tag) => (
              <span key={tag}>{tag}</span>
            ))}
          </div>
        )}
      </div>
      <dl className="spec-list">
        <div>
          <dt>{t("executor")}</dt>
          <dd>{t(detailTask.Executor)}</dd>
        </div>
        {mode !== "blackboard" && (
          <div>
            <dt>{t("role")}</dt>
            <dd>{detailTask.AllowedRoles?.join(", ") || t("unrestricted")}</dd>
          </div>
        )}
        <div>
          <dt>
            {t(
              responsibilityLabels[
                detail.data.Responsibility
                  .Kind as keyof typeof responsibilityLabels
              ] ?? "responsibility",
            )}
          </dt>
          <dd>
            {detail.data.Responsibility.Actor?.ID ||
              t(
                detail.data.Responsibility.Kind === "unclaimed"
                  ? "unclaimed"
                  : "notRecorded",
              )}
          </dd>
        </div>
      </dl>
      {detail.data.Outcome.Reason && (
        <div className="detail-block">
          <span>{t("outcomeReason")}</span>
          <p>{detail.data.Outcome.Reason}</p>
        </div>
      )}
      <div className="detail-block">
        <span>{t("acceptance")}</span>
        <p>{detailTask.AcceptanceCriteria || "—"}</p>
      </div>
      {latestResult && (
        <div className="result-block">
          <div>
            <ShieldCheck size={16} />
            <span>{t("latestResult")}</span>
          </div>
          <pre>{latestResult}</pre>
        </div>
      )}
      {detail.data.History.Reviews.length > 0 && (
        <div className="timeline">
          <span>{t("reviewChannel")}</span>
          {detail.data.History.Reviews.map((item) => (
            <div className="timeline-item" key={item.ID}>
              <i />
              <div>
                <div>
                  <strong>{reviewStatus(item.Status)}</strong>
                  <time>{formatTime(item.RequestedAt)}</time>
                </div>
                <p>
                  {item.Feedback ||
                    t("requestedBy", { actor: item.RequestedBy })}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
      {detail.data.History.Failures.length > 0 && (
        <div className="timeline danger">
          <span>{t("failureHistory")}</span>
          {detail.data.History.Failures.map((item) => (
            <div className="timeline-item" key={item.ID}>
              <i />
              <div>
                <strong>
                  {t(
                    item.Action === "reopen"
                      ? "actionReopen"
                      : "actionFailWorkItem",
                  )}
                </strong>
                <p>{item.Reason}</p>
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="task-operations">
        <ReviewActions
          task={operationTask}
          identity={identity}
          detail={detail.data}
          activeOperation={activeOperation}
          onSelectOperation={setActiveOperation}
        />
        {needsExecutionContext && execution.isLoading && (
          <OperationPanel
            operation={task.Status === "pending" ? "start" : "complete"}
            activeOperation={activeOperation}
            onSelect={setActiveOperation}
            title={t("preparingTask")}
          >
            <div className="human-actions loading-action">
              {t("preparingTask")}
            </div>
          </OperationPanel>
        )}
        {needsExecutionContext && execution.error && (
          <OperationPanel
            operation={task.Status === "pending" ? "start" : "complete"}
            activeOperation={activeOperation}
            onSelect={setActiveOperation}
            title={t("taskActionsUnavailable")}
          >
            <div className="human-actions">
              <p className="operation-description">
                {t("taskContextUnavailable")}
              </p>
              <FormError error={execution.error} />
              <button
                className="quiet-button action-primary"
                onClick={() => void execution.refetch()}
              >
                {t("retry")}
              </button>
            </div>
          </OperationPanel>
        )}
        {!execution.isLoading && !execution.error && (
          <HumanTaskActions
            task={operationTask}
            identity={identity}
            execution={execution}
            activeOperation={activeOperation}
            onSelectOperation={setActiveOperation}
          />
        )}
        {mode === "blackboard" && (
          <BlackboardPlanningActions
            task={operationTask}
            activeClaim={operationClaim}
            identity={identity}
            canDecompose={detail.data.Capabilities.CanDecompose && canDecompose}
            canSkip={detail.data.Capabilities.CanSkip}
            canAddChild={detail.data.Capabilities.CanAddChild}
            activeOperation={activeOperation}
            onSelectOperation={setActiveOperation}
          />
        )}
      </div>
    </div>
  );
}

function OperationPanel({
  operation,
  activeOperation,
  onSelect,
  title,
  children,
  danger = false,
}: {
  operation: TaskOperation;
  activeOperation: TaskOperation | null;
  onSelect: (operation: TaskOperation | null) => void;
  title: string;
  children: ReactNode;
  danger?: boolean;
}) {
  const open = operation === activeOperation;
  return (
    <section
      className={`operation-panel ${open ? "open" : ""} ${danger ? "danger" : ""}`}
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => onSelect(open ? null : operation)}
      >
        <span>{title}</span>
        <ChevronDown size={16} />
      </button>
      {open && <div className="operation-content">{children}</div>}
    </section>
  );
}

function HumanTaskActions({
  task,
  identity,
  execution,
  activeOperation,
  onSelectOperation,
}: {
  task: Task;
  identity: Identity;
  execution: UseQueryResult<TaskExecutionContext, Error>;
  activeOperation: TaskOperation | null;
  onSelectOperation: (operation: TaskOperation | null) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const canHumanExecute =
    task.Executor === "human" || task.Executor === "either";
  const [result, setResult] = useState("");
  const [artifactURIs, setArtifactURIs] = useState<Record<string, string>>({});
  const [createdArtifactIDs, setCreatedArtifactIDs] = useState<Record<string, string>>({});
  const [requestReview, setRequestReview] = useState(false);
  const [transitionID, setTransitionID] = useState("");
  const [failureReason, setFailureReason] = useState("");
  const [retryPrompt, setRetryPrompt] = useState("");
  const [failureAction, setFailureAction] = useState<
    "reopen" | "fail_work_item"
  >("reopen");

  const refresh = () =>
    refreshTaskState(queryClient, identity, task.ID, task.WorkItemID);
  const claim = useMutation({
    mutationFn: () => api.claimTask(identity, task.ID),
    onSuccess: refresh,
  });
  const activeClaim = execution.data?.Claims.find(
    (item) =>
      !item.EndedAt &&
      item.Executor.Kind === identity.kind &&
      item.Executor.ID === identity.id,
  );
  const choices = execution.data?.Workflow?.ChoiceGroups ?? [];
  const expectedArtifacts = execution.data?.ExpectedArtifacts ?? [];
  const stagedArtifacts = (execution.data?.Artifacts ?? []).filter(
    (artifact) =>
      artifact.ClaimID === activeClaim?.ID && !artifact.SubmissionID,
  );
  const selectedTransition = transitionID || choices[0]?.ID || "";
  const submit = useMutation({
    mutationFn: async () => {
      const created = { ...createdArtifactIDs };
      for (const expected of expectedArtifacts) {
        if (
          stagedArtifacts.some(
            (artifact) => artifact.Name === expected.Name,
          ) || created[expected.Name]
        )
          continue;
        const artifact = await api.createArtifact(identity, task.ID, {
          claim_id: activeClaim!.ID,
          name: expected.Name,
          uri: artifactURIs[expected.Name],
        });
        created[expected.Name] = artifact.ID;
        setCreatedArtifactIDs({ ...created });
      }
      return api.submitTask(identity, task.ID, {
        claim_id: activeClaim!.ID,
        result,
        artifact_ids: [
          ...new Set([
            ...stagedArtifacts.map((artifact) => artifact.ID),
            ...Object.values(created),
          ]),
        ],
        request_review: requestReview,
        transition: selectedTransition
          ? {
              choice_group_id: selectedTransition,
              skip_optional_task_ids: [],
              review_skipped_task_ids: [],
              reason: "",
          }
          : null,
      });
    },
    onSuccess: () => {
      setResult("");
      setArtifactURIs({});
      setCreatedArtifactIDs({});
      return refresh();
    },
  });
  const release = useMutation({
    mutationFn: () => api.releaseClaim(identity, task.ID, activeClaim!.ID),
    onSuccess: refresh,
  });
  const fail = useMutation({
    mutationFn: () =>
      api.failTask(identity, task.ID, {
        claim_id: activeClaim!.ID,
        action: failureAction,
        reason: failureReason,
        retry_prompt: failureAction === "reopen" ? retryPrompt : "",
      }),
    onSuccess: () => {
      setFailureReason("");
      setRetryPrompt("");
      setFailureAction("reopen");
      return refresh();
    },
  });

  if (
    !canHumanExecute ||
    (task.Status !== "pending" && task.Status !== "working")
  )
    return null;
  if (!execution.data) return null;
  if (task.Status === "pending")
    return (
      <OperationPanel
        operation="start"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("startTask")}
      >
        <div className="human-actions">
          <p className="operation-description">{t("startTaskBody")}</p>
          {claim.error && <FormError error={claim.error} />}
          <button
            className="primary-button action-primary"
            disabled={claim.isPending}
            onClick={() => claim.mutate()}
          >
            {t("startTask")}
          </button>
        </div>
      </OperationPanel>
    );
  if (!activeClaim) return null;

  const canChooseReview =
    execution.data.WorkItem.Definition.Mode === "blackboard" ||
    task.ReviewPolicy === "executor_decides";
  const artifactsReady = expectedArtifacts.every(
    (expected) =>
      stagedArtifacts.some((artifact) => artifact.Name === expected.Name) ||
      createdArtifactIDs[expected.Name] ||
      artifactURIs[expected.Name]?.trim(),
  );
  return (
    <>
      <OperationPanel
        operation="complete"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("completeTask")}
      >
        <div className="human-actions active-work">
          <p className="operation-description">{t("completeTaskBody")}</p>
          <label>
            {t("workResult")}
            <textarea
              rows={5}
              value={result}
              onChange={(event) => setResult(event.target.value)}
              placeholder={t("workResultPlaceholder")}
            />
          </label>
          {expectedArtifacts.length > 0 && (
            <div className="expected-artifacts">
              <strong>{t("expectedArtifacts")}</strong>
              {expectedArtifacts.map((expected) => {
                const existing = stagedArtifacts.find(
                  (artifact) => artifact.Name === expected.Name,
                );
                return (
                  <label key={expected.Name}>
                    {expected.Name}
                    <small>{expected.Description}</small>
                    {existing || createdArtifactIDs[expected.Name] ? (
                      <span className="artifact-ready">
                        {t("artifactReady")}
                      </span>
                    ) : (
                      <input
                        value={artifactURIs[expected.Name] ?? ""}
                        onChange={(event) =>
                          setArtifactURIs((current) => ({
                            ...current,
                            [expected.Name]: event.target.value,
                          }))
                        }
                        placeholder={t("artifactURIPlaceholder")}
                      />
                    )}
                  </label>
                );
              })}
            </div>
          )}
          {choices.length > 0 && (
            <label>
              {t("nextPath")}
              <select
                value={selectedTransition}
                onChange={(event) => setTransitionID(event.target.value)}
              >
                {choices.map((choice) => (
                  <option key={choice.ID} value={choice.ID}>
                    {choice.Targets.map((target) => target.Title).join(" + ")}
                  </option>
                ))}
              </select>
            </label>
          )}
          {canChooseReview && (
            <label className="review-choice">
              <input
                type="checkbox"
                checked={requestReview}
                onChange={(event) => setRequestReview(event.target.checked)}
              />
              <span>{t("requestReview")}</span>
            </label>
          )}
          {task.ReviewPolicy === "required" && (
            <p className="required-review">{t("reviewRequired")}</p>
          )}
          {submit.error && <FormError error={submit.error} />}
          <div className="human-action-buttons">
            <button
              className="primary-button"
              disabled={!result.trim() || !artifactsReady || submit.isPending}
              onClick={() => submit.mutate()}
            >
              {t("submitResult")}
            </button>
          </div>
        </div>
      </OperationPanel>
      <OperationPanel
        operation="release"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("putDownForNow")}
      >
        <div className="human-actions">
          <p className="operation-description">{t("putDownBody")}</p>
          {release.error && <FormError error={release.error} />}
          <button
            className="quiet-button action-primary"
            disabled={release.isPending}
            onClick={() => release.mutate()}
          >
            {t("putDownForNow")}
          </button>
        </div>
      </OperationPanel>
      <OperationPanel
        operation="fail"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("couldNotComplete")}
        danger
      >
        <div className="human-actions failure-form">
          <p className="operation-description">{t("failureActionBody")}</p>
          <label>
            {t("failureReason")}
            <textarea
              rows={3}
              value={failureReason}
              onChange={(event) => setFailureReason(event.target.value)}
            />
          </label>
          <fieldset className="failure-outcome">
            <legend>{t("failureOutcome")}</legend>
            <label>
              <input
                type="radio"
                name="failure-action"
                checked={failureAction === "reopen"}
                onChange={() => setFailureAction("reopen")}
              />
              <span>
                <strong>{t("makeAvailableAgain")}</strong>
                <small>{t("makeAvailableAgainBody")}</small>
              </span>
            </label>
            <label>
              <input
                type="radio"
                name="failure-action"
                checked={failureAction === "fail_work_item"}
                onChange={() => setFailureAction("fail_work_item")}
              />
              <span>
                <strong>{t("closeWorkAsFailed")}</strong>
                <small>{t("closeWorkAsFailedBody")}</small>
              </span>
            </label>
          </fieldset>
          {failureAction === "reopen" && (
            <label>
              {t("retryGuidance")}
              <textarea
                rows={2}
                value={retryPrompt}
                onChange={(event) => setRetryPrompt(event.target.value)}
              />
            </label>
          )}
          {fail.error && <FormError error={fail.error} />}
          <button
            className="quiet-button danger-button"
            disabled={!failureReason.trim() || fail.isPending}
            onClick={() => fail.mutate()}
          >
            {t(
              failureAction === "reopen"
                ? "recordAndReopen"
                : "confirmFailWorkItem",
            )}
          </button>
        </div>
      </OperationPanel>
    </>
  );
}

export function CreateTask({
  identity,
  workItemID,
}: {
  identity: Identity;
  workItemID: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const mutation = useMutation({
    mutationFn: (input: TaskDraftInput) =>
      api.createTask(identity, workItemID, input),
    onSuccess: async () => {
      await refreshWorkItemState(queryClient, identity, workItemID);
      setOpen(false);
    },
  });
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    mutation.mutate({
      title: formValue(data, "title"),
      description: formValue(data, "description"),
      acceptance_criteria: formValue(data, "acceptance"),
      executor: formValue(data, "executor") as Task["Executor"],
      allowed_roles: [],
      tags: splitValues(data.get("tags")),
    });
  }
  return (
    <>
      <button className="text-button" onClick={() => setOpen(true)}>
        <Plus size={15} />
        {t("addTask")}
      </button>
      <Modal
        open={open}
        onOpenChange={setOpen}
        title={t("addExecutionTask")}
        eyebrow={t("blackboardPlanning")}
      >
        <form className="form-grid" onSubmit={submit}>
          <label className="wide">
            {t("title")}
            <input name="title" required />
          </label>
          <label className="wide">
            {t("description")}
            <textarea name="description" rows={3} />
          </label>
          <label className="wide">
            {t("acceptanceCriteria")}
            <textarea name="acceptance" rows={2} />
          </label>
          <label className="wide">
            {t("executor")}
            <select name="executor">
              <option value="either">{t("either")}</option>
              <option value="human">{t("human")}</option>
              <option value="agent">{t("agent")}</option>
            </select>
          </label>
          <label className="wide">
            {t("tags")}
            <input name="tags" placeholder="backend, urgent" />
          </label>
          {mutation.error && <FormError error={mutation.error} />}
          <div className="form-actions">
            <button type="button" onClick={() => setOpen(false)}>
              {t("cancel")}
            </button>
            <button className="primary-button" disabled={mutation.isPending}>
              {t("createTask")}
            </button>
          </div>
        </form>
      </Modal>
    </>
  );
}

export function EmptyBlackboardActions({
  identity,
  workItemID,
}: {
  identity: Identity;
  workItemID: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [result, setResult] = useState("");
  const complete = useMutation({
    mutationFn: () => api.submitBlackboardCompletion(identity, workItemID, result),
    onSuccess: () => refreshWorkItemState(queryClient, identity, workItemID),
  });
  return (
    <div className="empty-planning">
      <CircleDot size={20} />
      <h3>{t("beginBlackboard")}</h3>
      <p>{t("beginBlackboardBody")}</p>
      <div>
        <CreateTask identity={identity} workItemID={workItemID} />
        <details>
          <summary>{t("completeWithoutTasks")}</summary>
          <label>
            {t("completionResult")}
            <textarea
              rows={3}
              value={result}
              onChange={(event) => setResult(event.target.value)}
            />
          </label>
          {complete.error && <FormError error={complete.error} />}
          <button
            className="quiet-button"
            disabled={!result.trim() || complete.isPending}
            onClick={() => complete.mutate()}
          >
            {t("submitCompletion")}
          </button>
        </details>
      </div>
    </div>
  );
}

export function BlackboardCompletionActions({
  identity,
  workItemID,
}: {
  identity: Identity;
  workItemID: string;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [result, setResult] = useState("");
  const submit = useMutation({
    mutationFn: () => api.submitBlackboardCompletion(identity, workItemID, result),
    onSuccess: () => refreshWorkItemState(queryClient, identity, workItemID),
  });
  return (
    <div className="empty-planning">
      <CircleDot size={20} />
      <h3>{t("blackboardConverged")}</h3>
      <p>{t("blackboardConvergedBody")}</p>
      <label>
        {t("completionResult")}
        <textarea rows={3} value={result} onChange={(event) => setResult(event.target.value)} />
      </label>
      {submit.error && <FormError error={submit.error} />}
      <button className="quiet-button" disabled={!result.trim() || submit.isPending} onClick={() => submit.mutate()}>
        {t("submitCompletion")}
      </button>
    </div>
  );
}

function TaskDraftFields({
  value,
  onChange,
  ordinal,
  removable,
  onRemove,
}: {
  value: TaskDraft;
  onChange: (next: TaskDraft) => void;
  ordinal?: number;
  removable?: boolean;
  onRemove?: () => void;
}) {
  const { t } = useI18n();
  const set = <K extends keyof TaskDraft>(key: K, next: TaskDraft[K]) =>
    onChange({ ...value, [key]: next });
  return (
    <div className="task-draft">
      <div className="task-draft-heading">
        <strong>
          {ordinal
            ? t("plannedTaskNumber", { number: ordinal })
            : t("plannedTask")}
        </strong>
        {removable && (
          <button
            type="button"
            className="icon-button"
            onClick={onRemove}
            aria-label={t("removeTask")}
          >
            <Trash2 size={15} />
          </button>
        )}
      </div>
      <label>
        {t("title")}
        <input
          className="task-title-input"
          required
          value={value.title}
          onChange={(event) => set("title", event.target.value)}
        />
      </label>
      <label>
        {t("description")}
        <textarea
          rows={3}
          value={value.description}
          onChange={(event) => set("description", event.target.value)}
        />
      </label>
      <label>
        {t("acceptanceCriteria")}
        <textarea
          rows={2}
          value={value.acceptance}
          onChange={(event) => set("acceptance", event.target.value)}
        />
      </label>
      <div className="task-draft-row">
        <label>
          {t("executor")}
          <select
            value={value.executor}
            onChange={(event) =>
              set("executor", event.target.value as Task["Executor"])
            }
          >
            <option value="either">{t("either")}</option>
            <option value="human">{t("human")}</option>
            <option value="agent">{t("agent")}</option>
          </select>
        </label>
        <label>
          {t("tags")}
          <input
            value={value.tags}
            onChange={(event) => set("tags", event.target.value)}
            placeholder="backend, urgent"
          />
        </label>
      </div>
    </div>
  );
}

function BlackboardPlanningActions({
  task,
  activeClaim,
  identity,
  canDecompose,
  canSkip,
  canAddChild,
  activeOperation,
  onSelectOperation,
}: {
  task: Task;
  activeClaim: Claim | null;
  identity: Identity;
  canDecompose: boolean;
  canSkip: boolean;
  canAddChild: boolean;
  activeOperation: TaskOperation | null;
  onSelectOperation: (operation: TaskOperation | null) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const [children, setChildren] = useState<TaskDraft[]>([emptyTaskDraft()]);
  const [child, setChild] = useState<TaskDraft>(emptyTaskDraft);
  const [skipReason, setSkipReason] = useState("");
  const refresh = () =>
    refreshTaskState(queryClient, identity, task.ID, task.WorkItemID);
  const decompose = useMutation({
    mutationFn: () =>
      api.decomposeBlackboardTask(identity, task.ID, {
        claim_id: activeClaim!.ID,
        children: children.map(taskDraftInput),
      }),
    onSuccess: () => {
      setChildren([emptyTaskDraft()]);
      return refresh();
    },
  });
  const addChild = useMutation({
    mutationFn: () =>
      api.addBlackboardChildTask(identity, task.ID, taskDraftInput(child)),
    onSuccess: () => {
      setChild(emptyTaskDraft());
      return refresh();
    },
  });
  const skip = useMutation({
    mutationFn: () => api.skipBlackboardTask(identity, task.ID, skipReason),
    onSuccess: () => {
      setSkipReason("");
      return refresh();
    },
  });
  const ownsClaim =
    activeClaim?.Executor.Kind === identity.kind &&
    activeClaim.Executor.ID === identity.id;
  const error = decompose.error ?? addChild.error ?? skip.error;
  if (task.Status === "working" && ownsClaim && canDecompose)
    return (
      <OperationPanel
        operation="decompose"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("breakIntoTasks")}
      >
        <div className="planning-action-body">
          <p className="operation-description">{t("breakIntoTasksBody")}</p>
          {children.map((draft, index) => (
            <TaskDraftFields
              key={index}
              value={draft}
              ordinal={index + 1}
              onChange={(next) =>
                setChildren((current) =>
                  current.map((item, itemIndex) =>
                    itemIndex === index ? next : item,
                  ),
                )
              }
              removable={children.length > 1}
              onRemove={() =>
                setChildren((current) =>
                  current.filter((_, itemIndex) => itemIndex !== index),
                )
              }
            />
          ))}
          <button
            type="button"
            className="inline-action"
            onClick={() =>
              setChildren((current) => [...current, emptyTaskDraft()])
            }
          >
            <Plus size={14} />
            {t("addAnotherTask")}
          </button>
          {error && <FormError error={error} />}
          <button
            className="primary-button planning-submit"
            disabled={
              children.some((item) => !item.title.trim()) || decompose.isPending
            }
            onClick={() => decompose.mutate()}
          >
            {t("confirmBreakdown")}
          </button>
        </div>
      </OperationPanel>
    );
  if (canAddChild && canAddBlackboardChild(task))
    return (
      <OperationPanel
        operation="add-child"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("addChildTask")}
      >
        <div className="planning-action-body">
          <p className="operation-description">{t("addChildTaskBody")}</p>
          <TaskDraftFields value={child} onChange={setChild} />
          {error && <FormError error={error} />}
          <button
            className="primary-button planning-submit"
            disabled={!child.title.trim() || addChild.isPending}
            onClick={() => addChild.mutate()}
          >
            {t("createChildTask")}
          </button>
        </div>
      </OperationPanel>
    );
  if (canSkip && canSkipBlackboardTask(task, activeClaim))
    return (
      <OperationPanel
        operation="skip"
        activeOperation={activeOperation}
        onSelect={onSelectOperation}
        title={t("noLongerNeeded")}
        danger
      >
        <div className="planning-action-body">
          <p className="operation-description">{t("noLongerNeededBody")}</p>
          <label>
            {t("skipReason")}
            <textarea
              rows={3}
              value={skipReason}
              onChange={(event) => setSkipReason(event.target.value)}
            />
          </label>
          {error && <FormError error={error} />}
          <button
            className="quiet-button danger-button"
            disabled={!skipReason.trim() || skip.isPending}
            onClick={() => skip.mutate()}
          >
            {t("skipTask")}
          </button>
        </div>
      </OperationPanel>
    );
  return null;
}

function ReviewActions({
  task,
  identity,
  detail,
  activeOperation,
  onSelectOperation,
}: {
  task: Task;
  identity: Identity;
  detail: TaskDetailView;
  activeOperation: TaskOperation | null;
  onSelectOperation: (operation: TaskOperation | null) => void;
}) {
  const { t } = useI18n();
  const queryClient = useQueryClient();
  const pendingReview =
    detail.Capabilities.CanReview && detail.CurrentReview?.Status === "pending"
      ? detail.CurrentReview
      : undefined;
  const [feedback, setFeedback] = useState("");
  const mutation = useMutation({
    mutationFn: (input: ReviewDecisionInput) =>
      api.decideReview(identity, task.ID, pendingReview!.ID, input),
    onSuccess: () => {
      setFeedback("");
      return refreshTaskState(queryClient, identity, task.ID, task.WorkItemID);
    },
  });
  if (task.Status !== "in_review" || !pendingReview) return null;
  return (
    <OperationPanel
      operation="review"
      activeOperation={activeOperation}
      onSelect={onSelectOperation}
      title={t("reviewThisResult")}
    >
      <div className="human-actions review-actions">
        <p className="operation-description">{t("reviewActionBody")}</p>
        <label>
          {t("feedback")}
          <textarea
            value={feedback}
            onChange={(event) => setFeedback(event.target.value)}
            rows={4}
            placeholder={t("rejectPlaceholder")}
          />
        </label>
        {mutation.error && <FormError error={mutation.error} />}
        <div className="decision-actions">
          <button
            className="reject-button"
            disabled={!feedback.trim() || mutation.isPending}
            onClick={() => mutation.mutate({ decision: "rejected", feedback })}
          >
            <X size={16} />
            {t("reject")}
          </button>
          <button
            className="approve-button"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate({ decision: "approved", feedback })}
          >
            <Check size={16} />
            {t("approve")}
          </button>
        </div>
      </div>
    </OperationPanel>
  );
}
