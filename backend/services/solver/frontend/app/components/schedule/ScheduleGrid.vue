<!-- app/components/schedule/ScheduleGrid.vue -->

<script setup lang="ts">
import type { SchoolModel, ScheduleSnapshot, TimeSpan } from '~/types/arrango'

interface CellItem {
  lessonId: number
  subject: string
  group: string
  division: string
  room: string
  teacher: string
  locked: boolean
}

interface BlockItem {
  name: string
}

const props = defineProps<{
  model: SchoolModel
  snapshot: ScheduleSnapshot | null
  mode: 'division' | 'teacher' | 'room'
  entityId: number
  // Issue highlighting: affected lessons + period spans and their severity.
  highlightLessons?: number[]
  highlightSpans?: TimeSpan[]
  highlightSeverity?: 'hard' | 'soft' | null
  // When true (an issue is focused), non-affected blocks are dimmed.
  dimOthers?: boolean
}>()

const emit = defineEmits<{ (e: 'lesson-click', lessonId: number): void }>()

const modelRef = computed(() => props.model)
const lookup = useSchoolLookup(modelRef as Ref<SchoolModel | null>)

const highlightLessonSet = computed(() => new Set(props.highlightLessons ?? []))
// Keys "dayId:period" (0-based period) for the affected spans.
const highlightCellSet = computed(() => {
  const s = new Set<string>()
  for (const span of props.highlightSpans ?? []) {
    for (let q = 0; q < Math.max(span.periodSpan, 1); q++) {
      s.add(`${span.dayId}:${span.startPeriod + q}`)
    }
  }
  return s
})
const highlighting = computed(
  () => highlightLessonSet.value.size > 0 || highlightCellSet.value.size > 0,
)

const maxPeriods = computed(() =>
  Math.max(0, ...props.model.days.map((d) => d.periodCount ?? 0)),
)

// cells[dayIndex][period] -> lessons occupying that slot for the entity.
const cells = computed(() => {
  const dayIndex = new Map(props.model.days.map((d, i) => [d.id, i]))
  const grid: CellItem[][][] = props.model.days.map(() =>
    Array.from({ length: maxPeriods.value }, () => []),
  )
  for (const scheduled of props.snapshot?.lessons ?? []) {
    const lesson = lookup.lessons.value.get(scheduled.lessonId)
    if (!lesson) continue
    const parts = lesson.participants ?? []
    // In division mode a lesson belongs to the entity when ANY participant
    // is in that division; the label shows that participant's division/group.
    const minePart =
      props.mode === 'division'
        ? parts.find((p) => p.divisionId === props.entityId)
        : parts[0]
    let mine = false
    if (props.mode === 'division') {
      mine = minePart !== undefined
    } else if (props.mode === 'teacher') {
      mine = lesson.requiresTeacher && lesson.teacherId === props.entityId
    } else {
      mine = (scheduled.placement?.roomId ?? 0) === props.entityId
    }
    if (!mine) continue
    const day = dayIndex.get(scheduled.placement?.dayId ?? 0)
    if (day === undefined) continue
    const start = scheduled.placement?.startPeriod ?? 0
    for (let q = start; q < start + (lesson.duration ?? 1); q++) {
      grid[day]?.[q]?.push({
        lessonId: lesson.id,
        subject: lookup.subjects.value.get(lesson.subjectId)?.name ?? '?',
        group: minePart?.groupId
          ? (lookup.groups.value.get(minePart.groupId)?.name ?? '?')
          : '',
        division: minePart
          ? (lookup.divisions.value.get(minePart.divisionId)?.name ?? '?')
          : (parts
              .map((p) => lookup.divisions.value.get(p.divisionId)?.name ?? '?')
              .join(', ') || '?'),
        room: scheduled.placement?.roomId
          ? (lookup.rooms.value.get(scheduled.placement.roomId)?.name ?? '?')
          : '',
        teacher: lesson.requiresTeacher
          ? (lookup.teachers.value.get(lesson.teacherId)?.name ?? '?')
          : '',
        locked: lesson.locked ?? false,
      })
    }
  }
  return grid
})

// External blocks shown as occupied slots for the selected entity.
const blocks = computed(() => {
  const dayIndex = new Map(props.model.days.map((d, i) => [d.id, i]))
  const grid: BlockItem[][][] = props.model.days.map(() =>
    Array.from({ length: maxPeriods.value }, () => []),
  )
  const groupIds = new Set(
    (props.model.groups ?? [])
      .filter((g) => g.divisionId === props.entityId)
      .map((g) => g.id),
  )
  for (const block of props.model.externalBlocks ?? []) {
    let hits = false
    if (props.mode === 'division') {
      hits =
        (block.target === 'TARGET_DIVISION' &&
          block.targetId === props.entityId) ||
        (block.target === 'TARGET_GROUP' && groupIds.has(block.targetId))
    } else if (props.mode === 'teacher') {
      hits = block.target === 'TARGET_TEACHER' && block.targetId === props.entityId
    } else {
      hits = block.target === 'TARGET_ROOM' && block.targetId === props.entityId
    }
    if (!hits) continue
    const day = dayIndex.get(block.dayId)
    if (day === undefined) continue
    const start = block.startPeriod ?? 0
    for (let q = start; q < start + (block.duration ?? 1); q++) {
      grid[day]?.[q]?.push({ name: block.name })
    }
  }
  return grid
})
</script>

<template>
  <v-table class="schedule-grid" density="compact">
    <thead>
      <tr>
        <th class="text-right" style="width: 48px">#</th>
        <th v-for="day in model.days" :key="day.id">{{ day.name }}</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="period in maxPeriods" :key="period">
        <td class="text-right text-medium-emphasis">{{ period }}</td>
        <td
          v-for="(day, dayIdx) in model.days"
          :key="day.id"
          class="schedule-cell"
          :class="{
            'cell--marked': highlightCellSet.has(`${day.id}:${period - 1}`),
            'cell--marked-hard':
              highlightSeverity === 'hard' &&
              highlightCellSet.has(`${day.id}:${period - 1}`),
          }"
        >
          <template v-if="period - 1 < (day.periodCount ?? 0)">
            <v-chip
              v-for="item in cells[dayIdx]?.[period - 1] ?? []"
              :key="item.lessonId"
              class="ma-1 lesson-chip"
              :class="{
                'lesson-chip--hit': highlightLessonSet.has(item.lessonId),
                'lesson-chip--dim':
                  dimOthers &&
                  highlighting &&
                  !highlightLessonSet.has(item.lessonId),
              }"
              :color="item.locked ? 'warning' : 'primary'"
              variant="tonal"
              role="button"
              tabindex="0"
              @click="emit('lesson-click', item.lessonId)"
              @keydown.enter="emit('lesson-click', item.lessonId)"
              @keydown.space.prevent="emit('lesson-click', item.lessonId)"
            >
              <v-icon v-if="item.locked" start size="x-small">mdi-lock</v-icon>
              <template v-if="mode !== 'division'">
                {{ item.division }}&nbsp;·&nbsp;
              </template>
              {{ item.subject }}
              <template v-if="item.group">&nbsp;({{ item.group }})</template>
              <template v-if="mode !== 'room' && item.room">
                &nbsp;· {{ item.room }}
              </template>
              <template v-if="mode !== 'teacher' && item.teacher">
                &nbsp;· {{ item.teacher }}
              </template>
            </v-chip>
            <v-chip
              v-for="(block, i) in blocks[dayIdx]?.[period - 1] ?? []"
              :key="`b${i}`"
              class="ma-1"
              color="grey"
              variant="tonal"
            >
              <v-icon start size="x-small">mdi-cancel</v-icon>
              {{ block.name }}
            </v-chip>
          </template>
          <span v-else class="text-disabled">—</span>
        </td>
      </tr>
    </tbody>
  </v-table>
</template>

<style scoped>
.schedule-cell {
  min-width: 160px;
  vertical-align: top;
  transition: background-color 0.2s ease;
}
/* A cell inside the focused issue's affected time span. */
.cell--marked {
  background: rgba(var(--v-theme-warning), 0.12);
  box-shadow: inset 2px 0 0 rgb(var(--v-theme-warning));
}
.cell--marked-hard {
  background: rgba(var(--v-theme-error), 0.12);
  box-shadow: inset 2px 0 0 rgb(var(--v-theme-error));
}
.lesson-chip {
  transition: opacity 0.2s ease, outline 0.2s ease;
}
/* A lesson block that is part of the focused issue. */
.lesson-chip--hit {
  outline: 2px solid rgb(var(--v-theme-error));
  outline-offset: 1px;
  font-weight: 600;
}
.lesson-chip--dim {
  opacity: 0.35;
}
</style>
