import { useState, type FormEvent } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CircleDot, Plus } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import { refreshWorkItemState } from "./taskOperations";
import type { Identity, Task, TaskDraftInput } from "./types";
import { FormError, Modal, formValue, splitValues } from "./ui";

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
    mutationFn: (input: TaskDraftInput) => api.createTask(identity, workItemID, input),
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
      executor: formValue(data, "executor") as Task["executor"],
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
        <textarea
          rows={3}
          value={result}
          onChange={(event) => setResult(event.target.value)}
        />
      </label>
      {submit.error && <FormError error={submit.error} />}
      <button
        className="quiet-button"
        disabled={!result.trim() || submit.isPending}
        onClick={() => submit.mutate()}
      >
        {t("submitCompletion")}
      </button>
    </div>
  );
}
