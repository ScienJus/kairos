import { describe, expect, it } from 'vitest'
import { latestPublishedDefinitions } from './AppModals'

type Definition = Parameters<typeof latestPublishedDefinitions>[0][number]

function definition(id: string, version: number, mode: Definition['mode'], status: Definition['status'] = 'published'): Definition {
  return { id, version, mode, status, name: id, description: '', agent_instructions: '', suggested_tags: [] }
}

describe('Definition selection', () => {
  it('offers only the latest version of each Definition and mode', () => {
    const result = latestPublishedDefinitions([
      definition('delivery', 1, 'blackboard'),
      definition('delivery', 3, 'blackboard'),
      definition('delivery', 2, 'blackboard'),
      definition('delivery', 1, 'workflow'),
    ])

    expect(result.map(item => `${item.mode}:${item.id}:v${item.version}`)).toEqual([
      'blackboard:delivery:v3',
      'workflow:delivery:v1',
    ])
  })

  it('does not fall back to an older published version when the latest is unavailable', () => {
    const result = latestPublishedDefinitions([
      definition('delivery', 1, 'blackboard'),
      definition('delivery', 2, 'blackboard', 'archived'),
      definition('research', 1, 'blackboard', 'draft'),
    ])

    expect(result).toEqual([])
  })
})
