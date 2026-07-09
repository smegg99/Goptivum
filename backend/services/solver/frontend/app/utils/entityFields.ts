// app/utils/entityFields.ts

import type { SchoolModel } from '~/types/arrango'
import type { EntityKind } from '~/utils/entityOps'

export interface FieldSpec {
  key: string
  label: string
  type: 'text' | 'number' | 'boolean' | 'ref'
  refKind?: EntityKind
  required?: boolean
}

export function entityCollection(kind: EntityKind): keyof SchoolModel {
  return ({
    year: 'years',
    division: 'divisions',
    group: 'groups',
    teacher: 'teachers',
    subject: 'subjects',
    room: 'rooms',
    lesson: 'lessons',
  } as const)[kind]
}

export const ENTITY_LABELS: Record<EntityKind, { singular: string, plural: string }> = {
  year: { singular: 'year', plural: 'years' },
  division: { singular: 'division', plural: 'divisions' },
  group: { singular: 'group', plural: 'groups' },
  teacher: { singular: 'teacher', plural: 'teachers' },
  subject: { singular: 'subject', plural: 'subjects' },
  room: { singular: 'room', plural: 'rooms' },
  lesson: { singular: 'lesson', plural: 'lessons' },
}

export const FIELD_SPECS: Record<EntityKind, FieldSpec[]> = {
  year: [
    { key: 'name', label: 'Name', type: 'text', required: true },
    { key: 'level', label: 'Level', type: 'number', required: true },
    { key: 'priority', label: 'Priority', type: 'number', required: true },
  ],
  division: [
    { key: 'name', label: 'Name', type: 'text', required: true },
    { key: 'yearId', label: 'Year', type: 'ref', refKind: 'year', required: true },
  ],
  group: [
    { key: 'name', label: 'Name', type: 'text', required: true },
    { key: 'divisionId', label: 'Division', type: 'ref', refKind: 'division', required: true },
    { key: 'splitId', label: 'Split id', type: 'number' },
  ],
  teacher: [
    { key: 'name', label: 'Name', type: 'text', required: true },
  ],
  subject: [
    { key: 'name', label: 'Name', type: 'text', required: true },
    { key: 'prefersBlocks', label: 'Prefers blocks', type: 'boolean' },
  ],
  room: [
    { key: 'name', label: 'Name', type: 'text', required: true },
    { key: 'designator', label: 'Designator', type: 'text', required: true },
  ],
  lesson: [
    { key: 'subjectId', label: 'Subject', type: 'ref', refKind: 'subject', required: true },
    { key: 'teacherId', label: 'Teacher', type: 'ref', refKind: 'teacher' },
    { key: 'divisionId', label: 'Division', type: 'ref', refKind: 'division', required: true },
    { key: 'groupId', label: 'Group', type: 'ref', refKind: 'group' },
    { key: 'duration', label: 'Duration', type: 'number', required: true },
    { key: 'requiresTeacher', label: 'Requires teacher', type: 'boolean' },
    { key: 'requiresRoom', label: 'Requires room', type: 'boolean' },
  ],
}
