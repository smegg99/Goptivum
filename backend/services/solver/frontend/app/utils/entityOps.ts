// app/utils/entityOps.ts

import type { LessonInstance, SchoolModel } from '~/types/arrango'
import { nextId } from '~/utils/model'

export type EntityKind =
  | 'year'
  | 'division'
  | 'group'
  | 'teacher'
  | 'subject'
  | 'room'
  | 'lesson'

export interface RemoveResult {
  ok: boolean
  blockers: string[]
}

export function addYear(
  model: SchoolModel,
  value: { name: string, level: number, priority: number },
): number {
  const id = nextId(model)
  model.years.push({ id, ...value })
  return id
}

export function addDivision(
  model: SchoolModel,
  value: { name: string, yearId: number },
): number {
  const id = nextId(model)
  model.divisions.push({ id, ...value })
  return id
}

export function addGroup(
  model: SchoolModel,
  value: { name: string, divisionId: number, splitId?: number },
): number {
  const id = nextId(model)
  model.groups.push({ id, ...value })
  return id
}

export function addTeacher(model: SchoolModel, value: { name: string }): number {
  const id = nextId(model)
  model.teachers.push({ id, ...value })
  return id
}

export function addSubject(
  model: SchoolModel,
  value: { name: string, prefersBlocks: boolean },
): number {
  const id = nextId(model)
  model.subjects.push({ id, ...value })
  return id
}

export function addRoom(
  model: SchoolModel,
  value: { name: string, designator: string },
): number {
  const id = nextId(model)
  model.rooms.push({ id, ...value })
  return id
}

export function addLesson(
  model: SchoolModel,
  partial: Partial<LessonInstance>,
): number {
  const id = nextId(model)
  model.lessons.push({
    id,
    participants: partial.participants ?? [],
    subjectId: partial.subjectId ?? 0,
    teacherId: partial.teacherId ?? 0,
    duration: partial.duration ?? 1,
    allowedRoomDesignators: partial.allowedRoomDesignators ?? [],
    disallowedRoomDesignators: partial.disallowedRoomDesignators ?? [],
    observedRoomDesignators: partial.observedRoomDesignators ?? [],
    fixedRoomId: partial.fixedRoomId,
    parallelBlockId: partial.parallelBlockId ?? 0,
    requiresTeacher: partial.requiresTeacher ?? Boolean(partial.teacherId),
    requiresRoom: partial.requiresRoom ?? true,
    locked: partial.locked ?? false,
    lockedPlacement: partial.lockedPlacement,
    hasPrevious: partial.hasPrevious ?? false,
    previousPlacement: partial.previousPlacement,
  })
  return id
}

export const ENTITY_COLLECTION = {
  year: 'years',
  division: 'divisions',
  group: 'groups',
  teacher: 'teachers',
  subject: 'subjects',
  room: 'rooms',
  lesson: 'lessons',
} as const satisfies Record<EntityKind, keyof SchoolModel>

export function updateEntity(
  model: SchoolModel,
  kind: EntityKind,
  id: number,
  patch: Record<string, unknown>,
): void {
  const list = model[ENTITY_COLLECTION[kind]] as Array<{ id: number }>
  const entity = list.find((item) => item.id === id)
  if (entity) Object.assign(entity, patch)
}

export function removeEntity(
  model: SchoolModel,
  kind: EntityKind,
  id: number,
): RemoveResult {
  switch (kind) {
    case 'year':
      return removeYear(model, id)
    case 'division':
      return removeDivision(model, id)
    case 'group':
      return removeGroup(model, id)
    case 'subject':
      return removeSubject(model, id)
    case 'teacher':
      return removeTeacher(model, id)
    case 'room':
      return removeRoom(model, id)
    case 'lesson':
      model.lessons = model.lessons.filter((lesson) => lesson.id !== id)
      return { ok: true, blockers: [] }
  }
}

function removeYear(model: SchoolModel, id: number): RemoveResult {
  const divisions = model.divisions.filter((division) => division.yearId === id)
  if (divisions.length) {
    return {
      ok: false,
      blockers: [`${divisions.length} division(s): ${divisions.map((d) => d.name).join(', ')}`],
    }
  }

  model.years = model.years.filter((year) => year.id !== id)
  return { ok: true, blockers: [] }
}

function removeDivision(model: SchoolModel, id: number): RemoveResult {
  model.groups = model.groups.filter((group) => group.divisionId !== id)
  model.lessons = model.lessons.filter((lesson) => (
    !lesson.participants.some((participant) => participant.divisionId === id)
  ))
  model.divisions = model.divisions.filter((division) => division.id !== id)
  return { ok: true, blockers: [] }
}

function removeGroup(model: SchoolModel, id: number): RemoveResult {
  for (const lesson of model.lessons) {
    lesson.participants = lesson.participants.filter((participant) => participant.groupId !== id)
  }
  model.lessons = model.lessons.filter((lesson) => lesson.participants.length > 0)
  model.groups = model.groups.filter((group) => group.id !== id)
  return { ok: true, blockers: [] }
}

function removeSubject(model: SchoolModel, id: number): RemoveResult {
  const count = model.lessons.filter((lesson) => lesson.subjectId === id).length
  if (count) {
    return {
      ok: false,
      blockers: [`${count} lesson${count === 1 ? '' : 's'} use this subject`],
    }
  }

  model.subjects = model.subjects.filter((subject) => subject.id !== id)
  return { ok: true, blockers: [] }
}

function removeTeacher(model: SchoolModel, id: number): RemoveResult {
  const count = model.lessons.filter((lesson) => (
    lesson.requiresTeacher && lesson.teacherId === id
  )).length
  if (count) {
    return {
      ok: false,
      blockers: [`${count} lesson${count === 1 ? '' : 's'} use this teacher`],
    }
  }

  model.teachers = model.teachers.filter((teacher) => teacher.id !== id)
  return { ok: true, blockers: [] }
}

function removeRoom(model: SchoolModel, id: number): RemoveResult {
  const designator = model.rooms.find((room) => room.id === id)?.designator

  for (const lesson of model.lessons) {
    if (designator) {
      lesson.allowedRoomDesignators = (lesson.allowedRoomDesignators ?? [])
        .filter((item) => item !== designator)
      lesson.disallowedRoomDesignators = (lesson.disallowedRoomDesignators ?? [])
        .filter((item) => item !== designator)
    }
    if (lesson.fixedRoomId === id) lesson.fixedRoomId = undefined
  }

  model.rooms = model.rooms.filter((room) => room.id !== id)
  return { ok: true, blockers: [] }
}
