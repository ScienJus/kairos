import { describe, expect, it, vi } from 'vitest'
import { ArtifactPreviewError, artifactPreviewMaxBytes, readArtifactPreview } from './artifactText'

describe('bounded artifact text reader', () => {
  it.each(['', 'Plain text\n第二行\tworks\r\n', '# Delivery\n\n- reviewed'])('reads UTF-8 text %j', async text => {
    await expect(readArtifactPreview(new Response(text))).resolves.toBe(text)
  })

  it('accepts the exact byte limit including multibyte UTF-8', async () => {
    const text = '界'.repeat(Math.floor(artifactPreviewMaxBytes / 3)) + 'a'.repeat(artifactPreviewMaxBytes % 3)
    expect(new TextEncoder().encode(text).byteLength).toBe(artifactPreviewMaxBytes)
    await expect(readArtifactPreview(new Response(text))).resolves.toBe(text)
    await expect(readArtifactPreview(new Response(text + 'a'))).rejects.toMatchObject({ reason: 'too_large' })
  })

  it('decodes multibyte characters split across chunks', async () => {
    const bytes = new TextEncoder().encode('前言\n# Result')
    const body = new ReadableStream<Uint8Array>({ start(controller) {
      controller.enqueue(bytes.slice(0, 2))
      controller.enqueue(bytes.slice(2))
      controller.close()
    } })
    await expect(readArtifactPreview(new Response(body))).resolves.toBe('前言\n# Result')
  })

  it('cancels on the first over-limit chunk without consuming the rest', async () => {
    const cancel = vi.fn()
    let reads = 0
    const body = new ReadableStream<Uint8Array>({
      pull(controller) { controller.enqueue(new Uint8Array(reads++ === 0 ? artifactPreviewMaxBytes : 1).fill(65)) },
      cancel,
    }, { highWaterMark: 0 })
    await expect(readArtifactPreview(new Response(body))).rejects.toMatchObject({ reason: 'too_large' })
    expect(cancel).toHaveBeenCalledTimes(1)
    expect(reads).toBe(2)
  })

  it('cancels a declared oversized response before reading it', async () => {
    const cancel = vi.fn()
    const response = new Response(new ReadableStream({ cancel }), { headers: { 'Content-Length': String(artifactPreviewMaxBytes + 1) } })
    const getReader = vi.spyOn(response.body!, 'getReader')
    await expect(readArtifactPreview(response)).rejects.toMatchObject({ reason: 'too_large' })
    expect(getReader).not.toHaveBeenCalled()
    expect(cancel).toHaveBeenCalledTimes(1)
  })

  it.each([new Uint8Array([0xff]), new Uint8Array([0xe4, 0xb8]), new Uint8Array([65, 0, 66]), new Uint8Array([65, 7])])('rejects non-text bytes %j', async bytes => {
    await expect(readArtifactPreview(new Response(bytes))).rejects.toBeInstanceOf(ArtifactPreviewError)
    await expect(readArtifactPreview(new Response(bytes))).rejects.toMatchObject({ reason: 'not_text' })
  })

  it('preserves a stream read failure and cancels the reader', async () => {
    const response = new Response(new ReadableStream({ pull() { throw new Error('Connection interrupted') } }))
    await expect(readArtifactPreview(response)).rejects.toThrow('Connection interrupted')
  })
})
