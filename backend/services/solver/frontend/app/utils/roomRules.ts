// app/utils/roomRules.ts

import type { SchoolModel } from '~/types/arrango'

export interface SubjectRoomRule {
  allowed: string[]
  disallowed: string[]
}

export type SubjectRoomRules = Record<number, SubjectRoomRule>

export function applySubjectRule(
  model: SchoolModel,
  subjectId: number,
  rule: SubjectRoomRule,
): void {
  for (const lesson of model.lessons) {
    if (lesson.subjectId !== subjectId || !lesson.requiresRoom) continue
    lesson.allowedRoomDesignators = unique(rule.allowed)
    lesson.disallowedRoomDesignators = unique(rule.disallowed)
  }
}

export function applyAllRules(model: SchoolModel, rules: SubjectRoomRules): void {
  for (const [subjectId, rule] of Object.entries(rules)) {
    applySubjectRule(model, Number(subjectId), rule)
  }
}

export function deriveSubjectRule(model: SchoolModel, subjectId: number): SubjectRoomRule {
  const allowed = new Set<string>()
  const disallowed = new Set<string>()

  for (const lesson of model.lessons) {
    if (lesson.subjectId !== subjectId) continue
    for (const designator of lesson.allowedRoomDesignators ?? []) allowed.add(designator)
    for (const designator of lesson.disallowedRoomDesignators ?? []) disallowed.add(designator)
  }

  return { allowed: [...allowed], disallowed: [...disallowed] }
}

function unique(values: string[]): string[] {
  return [...new Set(values.filter(Boolean))]
}
