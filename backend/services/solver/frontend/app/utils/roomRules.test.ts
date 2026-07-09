// app/utils/roomRules.test.ts

import { beforeEach, describe, expect, it } from 'vitest'
import type { SchoolModel } from '~/types/arrango'
import { addDivision, addLesson, addSubject, addYear } from '~/utils/entityOps'
import { emptyModel } from '~/utils/model'
import { applySubjectRule, deriveSubjectRule } from '~/utils/roomRules'

let model: SchoolModel
let subject: number
let division: number

beforeEach(() => {
  model = emptyModel()
  const year = addYear(model, { name: 'Y1', level: 1, priority: 100 })
  division = addDivision(model, { name: '1a', yearId: year })
  subject = addSubject(model, { name: 'wf', prefersBlocks: false })
})

describe('applySubjectRule', () => {
  it('writes designators onto every room-requiring lesson of the subject', () => {
    const lessonOne = addLesson(model, {
      participants: [{ divisionId: division }],
      subjectId: subject,
    })
    const lessonTwo = addLesson(model, {
      participants: [{ divisionId: division }],
      subjectId: subject,
      requiresRoom: false,
    })

    applySubjectRule(model, subject, { allowed: ['sg1', 'sg2'], disallowed: [] })

    expect(model.lessons.find((lesson) => lesson.id === lessonOne)!.allowedRoomDesignators)
      .toEqual(['sg1', 'sg2'])
    expect(model.lessons.find((lesson) => lesson.id === lessonTwo)!.allowedRoomDesignators)
      .toEqual([])
  })
})

describe('deriveSubjectRule', () => {
  it('unions the designators currently on the subject lessons', () => {
    addLesson(model, {
      participants: [{ divisionId: division }],
      subjectId: subject,
      allowedRoomDesignators: ['sg1'],
    })
    addLesson(model, {
      participants: [{ divisionId: division }],
      subjectId: subject,
      allowedRoomDesignators: ['sg1', 'sg2'],
    })

    expect(deriveSubjectRule(model, subject).allowed.sort()).toEqual(['sg1', 'sg2'])
  })
})
