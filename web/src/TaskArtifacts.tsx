import { useState } from "react";
import { Download, ExternalLink, Paperclip } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import type { Identity, TaskDetailView } from "./types";
import { FormError } from "./ui";

export function TaskArtifacts({
  artifacts,
  identity,
}: {
  artifacts: TaskDetailView["artifacts"];
  identity: Identity;
}) {
  const { t } = useI18n();
  const [error, setError] = useState<Error | null>(null);

  async function download(artifact: TaskDetailView["artifacts"][number]) {
    try {
      setError(null);
      const blob = await api.downloadArtifact(identity, artifact.id);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = artifact.name;
      link.click();
      URL.revokeObjectURL(url);
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error(t("unreachable")));
    }
  }

  return (
    <section className="task-artifacts" aria-label={t("taskArtifacts")}>
      <div className="task-artifacts-heading">
        <Paperclip size={15} />
        <span>{t("taskArtifacts")}</span>
      </div>
      <div className="task-artifact-list">
        {artifacts.map((artifact) => (
          <div key={artifact.id}>
            <strong>{artifact.name}</strong>
            {artifact.uri.startsWith("kairos://") ? (
              <button
                type="button"
                className="quiet-button"
                onClick={() => void download(artifact)}
              >
                <Download size={14} />
                {t("downloadArtifact")}
              </button>
            ) : /^https?:\/\//.test(artifact.uri) ? (
              <a href={artifact.uri} target="_blank" rel="noreferrer">
                <span>{artifact.uri}</span>
                <ExternalLink size={13} />
              </a>
            ) : (
              <span>{artifact.uri}</span>
            )}
          </div>
        ))}
      </div>
      {error && <FormError error={error} />}
    </section>
  );
}
