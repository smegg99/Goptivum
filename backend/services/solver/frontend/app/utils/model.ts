// app/utils/model.ts

import type { SchoolModel, Weights } from '~/types/arrango'

const DAY_NAMES = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday']

export function zeroWeights(): Weights {
  return {
    studentGapBase: '0',
    teacherGapBase: '0',
    lateStudentLessonBase: '0',
    lateTeacherFinishBase: '0',
    subjectSplitBase: '0',
    blockBreakBase: '0',
    roomChangeBase: '0',
    stabilityMoveBase: '0',
    expectedBadPerLesson: '0',
    lateThresholdPeriod: 0,
    gapCapPerDay: 0,
  }
}

export function emptyModel(): SchoolModel {
  const days = DAY_NAMES.map((name, index) => ({
    id: index + 1,
    name,
    periodCount: 8,
  }))
  const periods = Array.from({ length: 8 }, (_, index) => ({
    id: 101 + index,
    name: String(index + 1),
  }))

  return {
    name: 'New school',
    days,
    periods,
    years: [],
    divisions: [],
    groups: [],
    teachers: [],
    subjects: [],
    rooms: [],
    lessons: [],
    externalBlocks: [],
    weights: zeroWeights(),
    preferences: [],
    dailyLoadRules: [],
  }
}

export function nextId(model: SchoolModel): number {
  const ids = [
    ...model.days,
    ...model.periods,
    ...model.years,
    ...model.divisions,
    ...model.groups,
    ...model.teachers,
    ...model.subjects,
    ...model.rooms,
    ...model.lessons,
    ...model.externalBlocks,
  ].map((entity) => entity.id)

  return Math.max(0, ...ids) + 1
}

export function deepClone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}
