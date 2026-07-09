// app/composables/useIssues.ts
//
// Normalizes hard conflicts and soft issues into one structured, positional
// model and holds the cross-page "focused issue" that the schedule grid
// highlights. Labels for kinds/categories/entity kinds live here (one place
// to translate), so the rest of the UI never parses solver prose.

import type {
  Conflict,
  EntityKind,
  EntityRef,
  LessonRef,
  SchoolModel,
  SoftIssue,
  TimeSpan,
} from '~/types/arrango'

export type Severity = 'hard' | 'soft'

export interface Issue {
  id: string
  severity: Severity
  // Machine code (KIND_* for hard, category for soft) and a readable label.
  code: string
  label: string
  penalty: number // 0 for hard conflicts
  count: number
  entities: EntityRef[]
  lessons: LessonRef[]
  spans: TimeSpan[]
  // Primary entity for jumping the schedule view (division/teacher/room).
  primary?: EntityRef
}

const HARD_LABELS: Record<string, string> = {
  KIND_TEACHER_CONFLICT: 'Teacher double-booked',
  KIND_ROOM_CONFLICT: 'Room double-booked',
  KIND_STUDENT_OVERLAP: 'Students double-booked',
  KIND_MISSING_LESSON: 'Lesson not scheduled',
  KIND_DUPLICATE_LESSON: 'Lesson scheduled twice',
  KIND_INVALID_ROOM: 'Room not allowed',
  KIND_ROOM_TOO_SMALL: 'Room too small',
  KIND_PARALLEL_BLOCK_BROKEN: 'Parallel block split',
  KIND_LOCKED_MOVED: 'Locked lesson moved',
  KIND_EXTERNAL_BLOCK_OVERLAP: 'Overlaps blocked time',
  KIND_OUT_OF_BOUNDS: 'Outside the day',
  KIND_INVALID_REFERENCE: 'Broken reference',
  KIND_INVALID_DURATION: 'Invalid duration',
  KIND_DAILY_LOAD_VIOLATION: 'Daily load',
  KIND_DUPLICATE_DESIGNATOR: 'Duplicate room code',
}

const SOFT_LABELS: Record<string, string> = {
  student_gap: 'Student gap',
  gap_window: 'Gap window',
  teacher_gap: 'Teacher gap',
  late_student: 'Late for students',
  late_teacher: 'Late for teacher',
  subject_split: 'Subject split',
  block_break: 'Block broken',
  room_change: 'Room change',
  stability: 'Moved from previous',
  max_lessons: 'Over daily max',
  prefer_early: 'Not early',
}

const ENTITY_META: Record<
  EntityKind,
  { label: string; icon: string; mode?: 'division' | 'teacher' | 'room' }
> = {
  ENTITY_KIND_UNSPECIFIED: { label: '', icon: 'mdi-help' },
  ENTITY_KIND_DIVISION: {
    label: 'Division',
    icon: 'mdi-account-group',
    mode: 'division',
  },
  ENTITY_KIND_GROUP: { label: 'Group', icon: 'mdi-account-multiple' },
  ENTITY_KIND_TEACHER: {
    label: 'Teacher',
    icon: 'mdi-human-male-board',
    mode: 'teacher',
  },
  ENTITY_KIND_ROOM: { label: 'Room', icon: 'mdi-door', mode: 'room' },
  ENTITY_KIND_SUBJECT: { label: 'Subject', icon: 'mdi-book-open-variant' },
  ENTITY_KIND_YEAR: { label: 'Year', icon: 'mdi-calendar' },
  ENTITY_KIND_EXTERNAL_BLOCK: { label: 'Blocked time', icon: 'mdi-cancel' },
}

export function entityKindMeta(kind: EntityKind) {
  return ENTITY_META[kind] ?? ENTITY_META.ENTITY_KIND_UNSPECIFIED
}

// Resolves a typed entity ref to a display name using the current model.
export function entityRefName(
  ref: EntityRef,
  model: SchoolModel | null,
): string {
  if (!model) return `#${ref.id}`
  const find = <T extends { id: number; name: string }>(arr?: T[]) =>
    arr?.find((e) => e.id === ref.id)?.name
  switch (ref.kind) {
    case 'ENTITY_KIND_DIVISION':
      return find(model.divisions) ?? `#${ref.id}`
    case 'ENTITY_KIND_GROUP':
      return find(model.groups) ?? `#${ref.id}`
    case 'ENTITY_KIND_TEACHER':
      return find(model.teachers) ?? `#${ref.id}`
    case 'ENTITY_KIND_ROOM':
      return find(model.rooms) ?? `#${ref.id}`
    case 'ENTITY_KIND_SUBJECT':
      return find(model.subjects) ?? `#${ref.id}`
    case 'ENTITY_KIND_EXTERNAL_BLOCK':
      return (
        model.externalBlocks?.find((b) => b.id === ref.id)?.name ?? `#${ref.id}`
      )
    default:
      return `#${ref.id}`
  }
}

function conflictToIssue(c: Conflict, i: number): Issue {
  const entities = c.entities ?? []
  return {
    id: `hard-${i}`,
    severity: 'hard',
    code: c.kind,
    label: HARD_LABELS[c.kind] ?? c.kind.replace('KIND_', '').toLowerCase(),
    penalty: 0,
    count: 1,
    entities,
    lessons: c.lessons ?? (c.lessonIds ?? []).map((id) => ({ lessonId: id })),
    spans: c.spans ?? (c.dayId ? [{ dayId: c.dayId, startPeriod: c.period, periodSpan: 1 }] : []),
    primary: entities[0],
  }
}

function softToIssue(s: SoftIssue, i: number): Issue {
  const entities = s.entities ?? []
  return {
    id: `soft-${i}`,
    severity: 'soft',
    code: s.category,
    label: SOFT_LABELS[s.category] ?? s.category.replaceAll('_', ' '),
    penalty: Number(s.penalty) || 0,
    count: s.count,
    entities,
    lessons: s.lessons ?? [],
    spans: s.spans ?? (s.dayId ? [{ dayId: s.dayId, startPeriod: s.period, periodSpan: Math.max(s.count, 1) }] : []),
    primary: entities[0],
  }
}

export function useIssues() {
  const { job } = useSolveJob()

  const hardIssues = computed<Issue[]>(() =>
    (job.value.latest?.validation?.conflicts ?? []).map(conflictToIssue),
  )
  const softIssues = computed<Issue[]>(() =>
    (job.value.latest?.score?.softIssues ?? []).map(softToIssue),
  )

  // Cross-page focus: clicking an issue highlights it on the schedule grid.
  const focused = useState<Issue | null>('focused-issue', () => null)
  function focusIssue(issue: Issue | null) {
    focused.value = issue
  }

  return { hardIssues, softIssues, focused, focusIssue }
}
