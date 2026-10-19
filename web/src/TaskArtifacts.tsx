import { lazy, Suspense, useState } from "react";
import { Download, ExternalLink, Eye } from "lucide-react";
import { api } from "./api";
import { useI18n } from "./i18n";
import type { Identity, TaskDetailView } from "./types";
import { FormError } from "./ui";

const ArtifactPreview = lazy(() => import('./ArtifactPreview'));

export function ArtifactLinks({
  artifacts,
  identity,
}: {
  artifacts: TaskDetailView["artifacts"];
  identity: Identity;
}) {
  const { t } = useI18n();
  const [error, setError] = useState<{ artifactID: string; cause: Error } | null>(null);
  const [previewID, setPreviewID] = useState<string | null>(null);
  const preview = artifacts.find(artifact => artifact.id === previewID);

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
      setError({ artifactID: artifact.id, cause: cause instanceof Error ? cause : new Error(t("unreachable")) });
    }
  }

  return (
    <div>
      <div className="task-artifact-list">
        {artifacts.map((artifact) => (
          <div key={artifact.id}>
            <strong>{artifact.name}</strong>
            {artifact.uri.startsWith("kairos://") ? (
              <div className="artifact-file-actions">
                <button type="button" className="quiet-button" aria-label={`${t('artifactPreview')}: ${artifact.name}`} onClick={() => { setError(null); setPreviewID(artifact.id); }}>
                  <Eye size={14} />{t('artifactPreview')}
                </button>
                <button type="button" className="quiet-button" onClick={() => void download(artifact)}>
                  <Download size={14} />
                  {t("downloadArtifact")}
                </button>
              </div>
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
      {error && !preview && <FormError error={error.cause} />}
      {preview && <Suspense fallback={<p role="status">{t('artifactPreviewLoading')}</p>}>
        <ArtifactPreview key={JSON.stringify([identity.kind, identity.id, identity.role, preview.id])} artifact={preview} identity={identity} onClose={() => setPreviewID(null)} onDownload={() => void download(preview)} downloadError={error?.artifactID === preview.id ? error.cause : null} />
      </Suspense>}
    </div>
  );
}
