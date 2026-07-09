// app/composables/useSchoolLookup.ts

import type { SchoolModel } from '~/types/arrango'

// Id -> entity lookups derived from the current model, for rendering.
export function useSchoolLookup(model: Ref<SchoolModel | null>) {
  const byId = <T extends { id: number }>(items: T[] | undefined) =>
    new Map((items ?? []).map((item) => [item.id, item]))

  const days = computed(() => byId(model.value?.days))
  const divisions = computed(() => byId(model.value?.divisions))
  const groups = computed(() => byId(model.value?.groups))
  const teachers = computed(() => byId(model.value?.teachers))
  const subjects = computed(() => byId(model.value?.subjects))
  const rooms = computed(() => byId(model.value?.rooms))
  const lessons = computed(() => byId(model.value?.lessons))

  function lessonLabel(lessonId: number): string {
    const lesson = lessons.value.get(lessonId)
    if (!lesson) return `#${lessonId}`
    const subject = subjects.value.get(lesson.subjectId)?.name ?? '?'
    // Join every participant division(/group); merged lessons show all.
    const who = (lesson.participants ?? [])
      .map((p) => {
        const cls = divisions.value.get(p.divisionId)?.name ?? '?'
        const group = p.groupId
          ? `/${groups.value.get(p.groupId)?.name ?? '?'}`
          : ''
        return `${cls}${group}`
      })
      .join(', ')
    return `${subject} ${who || '?'}`
  }

  return { days, divisions, groups, teachers, subjects, rooms, lessons, lessonLabel }
}
