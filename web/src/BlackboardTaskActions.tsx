import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import {
  canAddBlackboardChild,
  canSkipBlackboardTask,
  refreshTaskState,
} from "./taskOperations";
import {
  TaskOperationPanel,
  type TaskOperation,
} from "./TaskOperationPanel";
import type { Claim, Identity, Task } from "./types";
import { FormError, splitValues } from "./ui";

type TaskDraft = {
  title: string;
  description: string;
  acceptance: string;
  executor: Task["executor"];
  tags: string;
};

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
              set("executor", event.target.value as Task["executor"])
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

export function BlackboardTaskActions({
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
    refreshTaskState(queryClient, identity, task.id, task.work_item_id);
  const decompose = useMutation({
    mutationFn: () =>
      api.decomposeBlackboardTask(identity, task.id, {
        claim_id: activeClaim!.id,
        children: children.map(taskDraftInput),
      }),
    onSuccess: () => {
      setChildren([emptyTaskDraft()]);
      return refresh();
    },
  });
  const addChild = useMutation({
    mutationFn: () =>
      api.addBlackboardChildTask(identity, task.id, taskDraftInput(child)),
    onSuccess: () => {
      setChild(emptyTaskDraft());
      return refresh();
    },
  });
  const skip = useMutation({
    mutationFn: () => api.skipBlackboardTask(identity, task.id, skipReason),
    onSuccess: () => {
      setSkipReason("");
      return refresh();
    },
  });
  const ownsClaim =
    activeClaim?.executor.kind === identity.kind &&
    activeClaim.executor.id === identity.id;
  const error = decompose.error ?? addChild.error ?? skip.error;
  if (task.status === "working" && ownsClaim && canDecompose)
    return (
      <TaskOperationPanel
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
              children.some((item) => !item.title.trim()) ||
              decompose.isPending
            }
            onClick={() => decompose.mutate()}
          >
            {t("confirmBreakdown")}
          </button>
        </div>
      </TaskOperationPanel>
    );
  if (canAddChild && canAddBlackboardChild(task))
    return (
      <TaskOperationPanel
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
      </TaskOperationPanel>
    );
  if (canSkip && canSkipBlackboardTask(task, activeClaim))
    return (
      <TaskOperationPanel
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
      </TaskOperationPanel>
    );
  return null;
}
