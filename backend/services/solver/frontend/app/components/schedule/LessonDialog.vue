<!-- app/components/schedule/LessonDialog.vue -->

<script setup lang="ts">
import type { SchoolModel } from '~/types/arrango'

const props = defineProps<{
  model: SchoolModel
  lessonId: number | null
}>()

const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ (e: 'changed'): void }>()

const modelRef = computed(() => props.model)
const lookup = useSchoolLookup(modelRef as Ref<SchoolModel | null>)

const lesson = computed(() =>
  props.lessonId != null ? lookup.lessons.value.get(props.lessonId) : undefined,
)

const { job } = useSolveJob()
const currentPlacement = computed(() =>
  job.value.latest?.snapshot?.lessons.find(
    (sl) => sl.lessonId === props.lessonId,
  )?.placement,
)

const dayId = ref<number | null>(null)
const startPeriod = ref(0)
const roomId = ref<number | null>(null)

watch(open, (isOpen) => {
  if (!isOpen || !lesson.value) return
  const placement = lesson.value.lockedPlacement ?? currentPlacement.value
  dayId.value = placement?.dayId ?? props.model.days[0]?.id ?? null
  startPeriod.value = placement?.startPeriod ?? 0
  roomId.value = placement?.roomId || null
})

const dayItems = computed(() =>
  props.model.days.map((d) => ({ value: d.id, title: d.name })),
)
const periodItems = computed(() => {
  const day = props.model.days.find((d) => d.id === dayId.value)
  const count = day?.periodCount ?? 0
  return Array.from({ length: count }, (_, i) => ({
    value: i,
    title: `${i + 1}`,
  }))
})
const roomItems = computed(() =>
  (props.model.rooms ?? []).map((r) => ({ value: r.id, title: r.name })),
)

function lockHere() {
  if (!lesson.value || dayId.value == null) return
  lesson.value.locked = true
  lesson.value.lockedPlacement = {
    dayId: dayId.value,
    startPeriod: startPeriod.value,
    roomId: lesson.value.requiresRoom ? (roomId.value ?? 0) : 0,
  }
  emit('changed')
  open.value = false
}

function unlock() {
  if (!lesson.value) return
  lesson.value.locked = false
  lesson.value.lockedPlacement = undefined
  emit('changed')
  open.value = false
}
</script>

<template>
  <v-dialog v-model="open">
    <v-card v-if="lesson" :title="lookup.lessonLabel(lesson.id)">
      <v-card-text>
        <div class="text-body-2 mb-4 text-medium-emphasis">
          Teacher:
          {{
            lesson.requiresTeacher
              ? (lookup.teachers.value.get(lesson.teacherId)?.name ?? '?')
              : 'none required'
          }}
          · Duration: {{ lesson.duration ?? 1 }}
          <v-chip v-if="lesson.locked" color="warning" class="ml-2">
            <v-icon start size="x-small">mdi-lock</v-icon>
            locked
          </v-chip>
        </div>
        <v-row dense>
          <v-col cols="4">
            <v-select v-model="dayId" :items="dayItems" label="Day" />
          </v-col>
          <v-col cols="4">
            <v-select
              v-model="startPeriod"
              :items="periodItems"
              label="Start period"
            />
          </v-col>
          <v-col cols="4">
            <v-select
              v-model="roomId"
              :items="roomItems"
              :disabled="!lesson.requiresRoom"
              label="Room"
            />
          </v-col>
        </v-row>
        <div class="text-caption mt-2 text-medium-emphasis">
          Locking pins the lesson to this slot on the next solve. Re-solve to
          apply.
        </div>
      </v-card-text>
      <v-card-actions>
        <v-btn color="warning" prepend-icon="mdi-lock" @click="lockHere">
          Lock here
        </v-btn>
        <v-btn
          v-if="lesson.locked"
          variant="text"
          prepend-icon="mdi-lock-open"
          @click="unlock"
        >
          Unlock
        </v-btn>
        <v-spacer />
        <v-btn variant="text" @click="open = false">Close</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
