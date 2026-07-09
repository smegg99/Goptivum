// app/utils/workspaceStore.test.ts

import { describe, expect, it } from 'vitest'
import { addSubject } from '~/utils/entityOps'
import { emptyModel } from '~/utils/model'
import { deserializeWorkspace, serializeWorkspace } from '~/utils/workspaceStore'

describe('serialize/deserialize', () => {
  it('round-trips a workspace', () => {
    const model = emptyModel()
    const subject = addSubject(model, { name: 'mat', prefersBlocks: false })
    const workspace = {
      model,
      rules: { [subject]: { allowed: ['s1'], disallowed: [] } },
    }

    const restored = deserializeWorkspace(serializeWorkspace(workspace))

    expect(restored.model.subjects[0]!.name).toBe('mat')
    expect(restored.rules[subject]!.allowed).toEqual(['s1'])
  })

  it('drops rules for subjects that no longer exist', () => {
    const model = emptyModel()
    const workspace = {
      model,
      rules: { 999: { allowed: ['s1'], disallowed: [] } },
    }

    const restored = deserializeWorkspace(serializeWorkspace(workspace))

    expect(restored.rules[999]).toBeUndefined()
  })

  it('throws on malformed JSON', () => {
    expect(() => deserializeWorkspace('{not json')).toThrow()
  })
})
