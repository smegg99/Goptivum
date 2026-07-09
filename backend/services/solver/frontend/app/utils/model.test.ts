// app/utils/model.test.ts

import { describe, expect, it } from 'vitest'
import { deepClone, emptyModel, nextId, zeroWeights } from '~/utils/model'

describe('emptyModel', () => {
  it('has a 5-day 8-period calendar and empty entities', () => {
    const model = emptyModel()

    expect(model.days).toHaveLength(5)
    expect(model.days[0]!.periodCount).toBe(8)
    expect(model.periods).toHaveLength(8)
    expect(model.divisions).toEqual([])
    expect(model.lessons).toEqual([])

    const ids = [...model.days, ...model.periods].map((entity) => entity.id)
    expect(new Set(ids).size).toBe(ids.length)
  })
})

describe('nextId', () => {
  it('is 1 for an id-less model', () => {
    const model = emptyModel()
    model.days = []
    model.periods = []

    expect(nextId(model)).toBe(1)
  })

  it('is 1 + the max id across all collections', () => {
    const model = emptyModel()
    model.teachers = [{ id: 999, name: 'T' }]
    model.lessons = [
      {
        id: 1500,
        participants: [],
        subjectId: 0,
        teacherId: 0,
        duration: 1,
        parallelBlockId: 0,
        requiresTeacher: false,
        requiresRoom: false,
        locked: false,
        hasPrevious: false,
      },
    ]

    expect(nextId(model)).toBe(1501)
  })
})

describe('zeroWeights', () => {
  it('zeros every field', () => {
    const weights = zeroWeights()

    expect(weights.studentGapBase).toBe('0')
    expect(weights.gapCapPerDay).toBe(0)
  })
})

describe('deepClone', () => {
  it('produces an independent copy', () => {
    const model = emptyModel()
    const cloned = deepClone(model)
    cloned.days[0]!.name = 'Changed'

    expect(model.days[0]!.name).not.toBe('Changed')
  })
})
