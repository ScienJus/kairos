import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Check, X } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import { refreshTaskState } from "./taskOperations";
import {
  TaskOperationPanel,
  type TaskOperation,
} from "./TaskOperationPanel";
import type {
  Identity,
  ReviewDecisionInput,
  Task,
  TaskDetailView,
} from "./types";
import { FormError } from "./ui";

export function TaskReviewActions({
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
    detail.capabilities.can_review && detail.current_review?.status === "pending"
      ? detail.current_review
      : undefined;
  const [feedback, setFeedback] = useState("");
  const mutation = useMutation({
    mutationFn: (input: ReviewDecisionInput) =>
      api.decideReview(identity, task.id, pendingReview!.id, input),
    onSuccess: () => {
      setFeedback("");
      return refreshTaskState(queryClient, identity, task.id, task.work_item_id);
    },
  });
  if (task.status !== "in_review" || !pendingReview) return null;
  return (
    <TaskOperationPanel
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
    </TaskOperationPanel>
  );
}
