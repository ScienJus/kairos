export const artifactPreviewMaxBytes = 256 * 1024

export class ArtifactPreviewError extends Error {
  constructor(public reason: 'too_large' | 'not_text') { super(reason) }
}

export async function readArtifactPreview(response: Response): Promise<string> {
  if (Number(response.headers.get('Content-Length')) > artifactPreviewMaxBytes) {
    await response.body?.cancel().catch(() => {})
    throw new ArtifactPreviewError('too_large')
  }
  if (!response.body) return ''
  const reader = response.body.getReader()
  const bytes = new Uint8Array(artifactPreviewMaxBytes)
  let size = 0
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      if (size + value.byteLength > artifactPreviewMaxBytes) throw new ArtifactPreviewError('too_large')
      bytes.set(value, size)
      size += value.byteLength
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
  let text: string
  try {
    text = new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(0, size))
  } catch {
    throw new ArtifactPreviewError('not_text')
  }
  for (const character of text) {
    const code = character.charCodeAt(0)
    if ((code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127) throw new ArtifactPreviewError('not_text')
  }
  return text
}
