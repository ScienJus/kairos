import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ShieldCheck } from "lucide-react";
import { api } from "./api";
import { BlackboardTaskActions } from "./BlackboardTaskActions";
import { useI18n } from "./i18n";
import { TaskArtifacts } from "./TaskArtifacts";
import { TaskExecutionActions } from "./TaskExecutionActions";
import {
  TaskOperationPanel,
  type TaskOperation,
} from "./TaskOperationPanel";
import { TaskReviewActions } from "./TaskReviewActions";
import type { Claim, Identity, Review, Task } from "./types";
import { FormError, Status } from "./ui";

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
  identity,
  mode,
}: {
  task: Task;
  activeClaim: Claim | null;
  identity: Identity;
  mode: string;
}) {
  const { t, formatTime } = useI18n();
  const detail = useQuery({
    queryKey: ["task-detail", identity, task.id],
    queryFn: () => api.getTaskDetail(identity, task.id),
    retry: false,
  });
  const capabilities = detail.data?.capabilities;
  const needsExecutionContext =
    capabilities?.can_claim ||
    capabilities?.can_submit ||
    capabilities?.can_release ||
    capabilities?.can_fail ||
    capabilities?.can_decompose;
  const execution = useQuery({
    queryKey: ["task-context", identity, task.id],
    queryFn: () => api.getTaskContext(identity, task.id),
    enabled: needsExecutionContext === true,
    retry: false,
  });
  const detailTask = detail.data?.task ?? task;
  const hasPendingReview =
    capabilities?.can_review === true &&
    detail.data?.current_review?.status === "pending";
  const canDecompose = execution.data?.blackboard?.can_decompose === true;
  const operationTask = execution.data?.task ?? detailTask;
  const operationClaim =
    execution.data?.claims.find(
      (item) =>
        !item.ended_at &&
        item.executor.kind === identity.kind &&
        item.executor.id === identity.id,
    ) ?? activeClaim;
  const operationOwnsClaim =
    operationClaim?.executor.kind === identity.kind &&
    operationClaim.executor.id === identity.id;
  const defaultOperation: TaskOperation | null = hasPendingReview
    ? "review"
    : capabilities?.can_claim
      ? "start"
      : operationTask.status === "working" && operationOwnsClaim
        ? canDecompose
          ? "decompose"
          : "complete"
        : capabilities?.can_add_child
          ? "add-child"
          : capabilities?.can_skip
            ? "skip"
            : null;
  const [activeOperation, setActiveOperation] = useState<TaskOperation | null>(
    defaultOperation,
  );
  useEffect(() => {
    setActiveOperation(defaultOperation);
  }, [defaultOperation]);
  const latestResult = detail.data?.history.submissions.at(-1)?.result;
  const reviewStatus = (status: Review["status"]) =>
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
        <span>TASK / {shortID(detailTask.id)}</span>
        <Status value={detailTask.status} />
        <h3>{detailTask.title}</h3>
        <p>{detailTask.description || t("noDescription")}</p>
        {(detailTask.tags?.length ?? 0) > 0 && (
          <div className="task-tags detail" aria-label={t("tags")}>
            {detailTask.tags?.map((tag) => (
              <span key={tag}>{tag}</span>
            ))}
          </div>
        )}
      </div>
      <dl className="spec-list">
        <div>
          <dt>{t("executor")}</dt>
          <dd>{t(detailTask.executor)}</dd>
        </div>
        {mode !== "blackboard" && (
          <div>
            <dt>{t("role")}</dt>
            <dd>{detailTask.allowed_roles?.join(", ") || t("unrestricted")}</dd>
          </div>
        )}
        <div>
          <dt>
            {t(
              responsibilityLabels[
                detail.data.responsibility
                  .kind as keyof typeof responsibilityLabels
              ] ?? "responsibility",
            )}
          </dt>
          <dd>
            {detail.data.responsibility.actor?.id ||
              t(
                detail.data.responsibility.kind === "unclaimed"
                  ? "unclaimed"
                  : "notRecorded",
              )}
          </dd>
        </div>
      </dl>
      {detail.data.outcome.reason && (
        <div className="detail-block">
          <span>{t("outcomeReason")}</span>
          <p>{detail.data.outcome.reason}</p>
        </div>
      )}
      <div className="detail-block">
        <span>{t("acceptance")}</span>
        <p>{detailTask.acceptance_criteria || "—"}</p>
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
      {detail.data.artifacts.length > 0 && (
        <TaskArtifacts artifacts={detail.data.artifacts} identity={identity} />
      )}
      {detail.data.history.reviews.length > 0 && (
        <div className="timeline">
          <span>{t("reviewChannel")}</span>
          {detail.data.history.reviews.map((item) => (
            <div className="timeline-item" key={item.id}>
              <i />
              <div>
                <div>
                  <strong>{reviewStatus(item.status)}</strong>
                  <time>{formatTime(item.requested_at)}</time>
                </div>
                <p>
                  {item.feedback ||
                    t("requestedBy", { actor: item.requested_by })}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
      {detail.data.history.failures.length > 0 && (
        <div className="timeline danger">
          <span>{t("failureHistory")}</span>
          {detail.data.history.failures.map((item) => (
            <div className="timeline-item" key={item.id}>
              <i />
              <div>
                <strong>
                  {t(
                    item.action === "reopen"
                      ? "actionReopen"
                      : "actionFailWorkItem",
                  )}
                </strong>
                <p>{item.reason}</p>
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="task-operations">
        <TaskReviewActions
          task={operationTask}
          identity={identity}
          detail={detail.data}
          activeOperation={activeOperation}
          onSelectOperation={setActiveOperation}
        />
        {needsExecutionContext && execution.isLoading && (
          <TaskOperationPanel
            operation={task.status === "pending" ? "start" : "complete"}
            activeOperation={activeOperation}
            onSelect={setActiveOperation}
            title={t("preparingTask")}
          >
            <div className="human-actions loading-action">
              {t("preparingTask")}
            </div>
          </TaskOperationPanel>
        )}
        {needsExecutionContext && execution.error && (
          <TaskOperationPanel
            operation={task.status === "pending" ? "start" : "complete"}
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
          </TaskOperationPanel>
        )}
        {!execution.isLoading && !execution.error && (
          <TaskExecutionActions
            task={operationTask}
            identity={identity}
            execution={execution}
            activeOperation={activeOperation}
            onSelectOperation={setActiveOperation}
          />
        )}
        {mode === "blackboard" && (
          <BlackboardTaskActions
            task={operationTask}
            activeClaim={operationClaim}
            identity={identity}
            canDecompose={detail.data.capabilities.can_decompose && canDecompose}
            canSkip={detail.data.capabilities.can_skip}
            canAddChild={detail.data.capabilities.can_add_child}
            activeOperation={activeOperation}
            onSelectOperation={setActiveOperation}
          />
        )}
      </div>
    </div>
  );
}
