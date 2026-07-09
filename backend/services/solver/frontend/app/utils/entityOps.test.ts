// app/utils/entityOps.test.ts

import { beforeEach, describe, expect, it } from 'vitest'
import type { SchoolModel } from '~/types/arrango'
import {
  addDivision,
  addGroup,
  addLesson,
  addRoom,
  addSubject,
  addTeacher,
  addYear,
  removeEntity,
  updateEntity,
} from '~/utils/entityOps'
import { emptyModel } from '~/utils/model'

let model: SchoolModel

beforeEach(() => {
  model = emptyModel()
})

describe('add + id allocation', () => {
  it('allocates globally unique ids', () => {
    const year = addYear(model, { name: 'Y1', level: 1, priority: 100 })
    const division = addDivision(model, { name: '1a', yearId: year })
    const group = addGroup(model, { name: '1/2', divisionId: division })

    expect(new Set([year, division, group]).size).toBe(3)
    expect(model.groups[0]!.divisionId).toBe(division)
  })
})

describe('updateEntity', () => {
  it('merges a patch', () => {
    const teacher = addTeacher(model, { name: 'Kowalski' })

    updateEntity(model, 'teacher', teacher, { name: 'Nowak' })

    expect(model.teachers[0]!.name).toBe('Nowak')
  })
})

describe('removeEntity cascades and guards', () => {
  it('deleting a division removes its groups and lessons', () => {
    const year = addYear(model, { name: 'Y1', level: 1, priority: 100 })
    const division = addDivision(model, { name: '1a', yearId: year })
    const group = addGroup(model, { name: '1/2', divisionId: division })
    const subject = addSubject(model, { name: 'mat', prefersBlocks: false })
    addLesson(model, {
      participants: [{ divisionId: division, groupId: group }],
      subjectId: subject,
    })

    const result = removeEntity(model, 'division', division)

    expect(result.ok).toBe(true)
    expect(model.divisions).toHaveLength(0)
    expect(model.groups).toHaveLength(0)
    expect(model.lessons).toHaveLength(0)
  })

  it('refuses to delete a subject that lessons use', () => {
    const year = addYear(model, { name: 'Y1', level: 1, priority: 100 })
    const division = addDivision(model, { name: '1a', yearId: year })
    const subject = addSubject(model, { name: 'mat', prefersBlocks: false })
    addLesson(model, { participants: [{ divisionId: division }], subjectId: subject })

    const result = removeEntity(model, 'subject', subject)

    expect(result.ok).toBe(false)
    expect(result.blockers[0]).toContain('1 lesson')
    expect(model.subjects).toHaveLength(1)
  })

  it('deleting a room strips its designator from lessons', () => {
    const room = addRoom(model, { name: 'Gym', designator: 'sg1' })
    const year = addYear(model, { name: 'Y1', level: 1, priority: 100 })
    const division = addDivision(model, { name: '1a', yearId: year })
    const subject = addSubject(model, { name: 'wf', prefersBlocks: false })
    const lessonId = addLesson(model, {
      participants: [{ divisionId: division }],
      subjectId: subject,
      allowedRoomDesignators: ['sg1', 'sg2'],
    })

    const result = removeEntity(model, 'room', room)

    expect(result.ok).toBe(true)
    expect(model.lessons.find((lesson) => lesson.id === lessonId)!.allowedRoomDesignators)
      .toEqual(['sg2'])
  })
})
