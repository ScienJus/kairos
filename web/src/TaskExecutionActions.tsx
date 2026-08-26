import { useEffect, useRef, useState } from "react";
import {
  useMutation,
  useQueryClient,
  type UseQueryResult,
} from "@tanstack/react-query";
import { Link, Upload } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import { refreshTaskState } from "./taskOperations";
import {
  TaskOperationPanel,
  type TaskOperation,
} from "./TaskOperationPanel";
import type {
  Identity,
  Task,
  TaskExecutionContext,
} from "./types";
import { FormError } from "./ui";

type ArtifactDeliveryMode = "upload" | "uri";

function uniqueArtifactIDsByName(artifacts: TaskExecutionContext["artifacts"]) {
  const names = new Set<string>();
  return artifacts.flatMap((artifact) => {
    if (names.has(artifact.name)) return [];
    names.add(artifact.name);
    return [artifact.id];
  });
}

export function TaskExecutionActions({
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
    task.executor === "human" || task.executor === "either";
  const [result, setResult] = useState("");
  const [artifactModes, setArtifactModes] = useState<
    Record<string, ArtifactDeliveryMode>
  >({});
  const [artifactFiles, setArtifactFiles] = useState<Record<string, File>>({});
  const [artifactURIs, setArtifactURIs] = useState<Record<string, string>>({});
  const [createdArtifactIDs, setCreatedArtifactIDs] = useState<
    Record<string, string>
  >({});
  const artifactOperationIDs = useRef<Record<string, string>>({});
  const [requestReview, setRequestReview] = useState(false);
  const [transitionID, setTransitionID] = useState("");
  const [failureReason, setFailureReason] = useState("");
  const [retryPrompt, setRetryPrompt] = useState("");
  const [failureAction, setFailureAction] = useState<
    "reopen" | "fail_work_item"
  >("reopen");

  const refresh = () =>
    refreshTaskState(queryClient, identity, task.id, task.work_item_id);
  const claim = useMutation({
    mutationFn: () => api.claimTask(identity, task.id),
    onSuccess: refresh,
  });
  const activeClaim = execution.data?.claims.find(
    (item) =>
      !item.ended_at &&
      item.executor.kind === identity.kind &&
      item.executor.id === identity.id,
  );
  const resetClaimState = () => {
    setResult("");
    setArtifactModes({});
    setArtifactFiles({});
    setArtifactURIs({});
    setCreatedArtifactIDs({});
    artifactOperationIDs.current = {};
    setRequestReview(false);
    setTransitionID("");
    setFailureReason("");
    setRetryPrompt("");
    setFailureAction("reopen");
  };
  useEffect(() => {
    resetClaimState();
  }, [activeClaim?.id]);
  const choices = execution.data?.workflow?.choice_groups ?? [];
  const expectedArtifacts = execution.data?.expected_artifacts ?? [];
  const stagedArtifacts = (execution.data?.artifacts ?? []).filter(
    (artifact) =>
      artifact.claim_id === activeClaim?.id && !artifact.submission_id,
  );
  const selectedTransition = transitionID || choices[0]?.id || "";
  const submit = useMutation({
    mutationFn: async () => {
      const created = { ...createdArtifactIDs };
      for (const expected of expectedArtifacts) {
        if (
          stagedArtifacts.some((artifact) => artifact.name === expected.name) ||
          created[expected.name]
        )
          continue;
        const artifact =
          (artifactModes[expected.name] ?? "upload") === "upload"
            ? await api.uploadArtifact(
                identity,
                task.id,
                activeClaim!.id,
                expected.name,
                artifactFiles[expected.name],
                (artifactOperationIDs.current[expected.name] ??=
                  crypto.randomUUID()),
              )
            : await api.createArtifact(identity, task.id, {
                claim_id: activeClaim!.id,
                name: expected.name,
                uri: artifactURIs[expected.name],
              });
        created[expected.name] = artifact.id;
        setCreatedArtifactIDs({ ...created });
      }
      return api.submitTask(identity, task.id, {
        claim_id: activeClaim!.id,
        result,
        artifact_ids: [
          ...new Set([
            ...uniqueArtifactIDsByName(stagedArtifacts),
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
      resetClaimState();
      return refresh();
    },
  });
  const release = useMutation({
    mutationFn: () => api.releaseClaim(identity, task.id, activeClaim!.id),
    onSuccess: () => {
      resetClaimState();
      return refresh();
    },
  });
  const fail = useMutation({
    mutationFn: () =>
      api.failTask(identity, task.id, {
        claim_id: activeClaim!.id,
        action: failureAction,
        reason: failureReason,
        retry_prompt: failureAction === "reopen" ? retryPrompt : "",
      }),
    onSuccess: () => {
      resetClaimState();
      return refresh();
    },
  });

  if (
    !canHumanExecute ||
    (task.status !== "pending" && task.status !== "working")
  )
    return null;
  if (!execution.data) return null;
  if (task.status === "pending")
    return (
      <TaskOperationPanel
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
      </TaskOperationPanel>
    );
  if (!activeClaim) return null;

  const canChooseReview =
    execution.data.work_item.definition.mode === "blackboard" ||
    task.review_policy === "executor_decides";
  const artifactsReady = expectedArtifacts.every(
    (expected) =>
      stagedArtifacts.some((artifact) => artifact.name === expected.name) ||
      createdArtifactIDs[expected.name] ||
      ((artifactModes[expected.name] ?? "upload") === "upload"
        ? artifactFiles[expected.name]
        : artifactURIs[expected.name]?.trim()),
  );
  return (
    <>
      <TaskOperationPanel
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
                  (artifact) => artifact.name === expected.name,
                );
                const mode = artifactModes[expected.name] ?? "upload";
                return (
                  <div className="expected-artifact" key={expected.name}>
                    <strong>{expected.name}</strong>
                    <small>{expected.description}</small>
                    {existing || createdArtifactIDs[expected.name] ? (
                      <span className="artifact-ready">
                        {t("artifactReady")}
                      </span>
                    ) : (
                      <>
                        <div
                          className="artifact-delivery-modes"
                          aria-label={`${expected.name}: ${t("artifactDeliveryMode")}`}
                          role="group"
                        >
                          <button
                            type="button"
                            aria-pressed={mode === "upload"}
                            onClick={() =>
                              setArtifactModes((current) => ({
                                ...current,
                                [expected.name]: "upload",
                              }))
                            }
                          >
                            <Upload size={14} />
                            {t("uploadFile")}
                          </button>
                          <button
                            type="button"
                            aria-pressed={mode === "uri"}
                            onClick={() =>
                              setArtifactModes((current) => ({
                                ...current,
                                [expected.name]: "uri",
                              }))
                            }
                          >
                            <Link size={14} />
                            {t("externalURI")}
                          </button>
                        </div>
                        {mode === "upload" ? (
                          <label className="artifact-file-input">
                            <span>{t("uploadFile")}</span>
                            <input
                              type="file"
                              aria-label={`${expected.name}: ${t("uploadFile")}`}
                              onChange={(event) => {
                                const file = event.target.files?.[0];
                                delete artifactOperationIDs.current[
                                  expected.name
                                ];
                                setArtifactFiles((current) => {
                                  const next = { ...current };
                                  if (file) next[expected.name] = file;
                                  else delete next[expected.name];
                                  return next;
                                });
                              }}
                            />
                          </label>
                        ) : (
                          <label className="artifact-uri-input">
                            <span>{t("externalURI")}</span>
                            <input
                              value={artifactURIs[expected.name] ?? ""}
                              onChange={(event) =>
                                setArtifactURIs((current) => ({
                                  ...current,
                                  [expected.name]: event.target.value,
                                }))
                              }
                              placeholder={t("artifactURIPlaceholder")}
                            />
                          </label>
                        )}
                      </>
                    )}
                  </div>
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
                  <option key={choice.id} value={choice.id}>
                    {choice.targets.map((target) => target.title).join(" + ")}
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
          {task.review_policy === "required" && (
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
      </TaskOperationPanel>
      <TaskOperationPanel
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
      </TaskOperationPanel>
      <TaskOperationPanel
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
      </TaskOperationPanel>
    </>
  );
}
