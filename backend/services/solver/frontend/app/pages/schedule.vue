<!-- app/pages/schedule.vue -->

<script setup lang="ts">
import { entityRefName } from '~/composables/useIssues'

const { job, resolveCurrent, starting } = useSolveJob()
const { focused, focusIssue, hardIssues } = useIssues()

const mode = ref<'division' | 'teacher' | 'room'>('division')
const entityId = ref<number | null>(null)
const dialogOpen = ref(false)
const selectedLessonId = ref<number | null>(null)
const edited = ref(false)

const model = computed(() => job.value.model)
const snapshot = computed(() => job.value.latest?.snapshot ?? null)

const entityItems = computed(() => {
  if (!model.value) return []
  const source =
    mode.value === 'division'
      ? model.value.divisions
      : mode.value === 'teacher'
        ? model.value.teachers
        : model.value.rooms
  return (source ?? []).map((e) => ({ value: e.id, title: e.name }))
})

watch([model, mode], () => {
  if (!entityItems.value.some((i) => i.value === entityId.value)) {
    entityId.value = entityItems.value[0]?.value ?? null
  }
})
onMounted(() => {
  entityId.value = entityItems.value[0]?.value ?? null
  applyFocus()
})

// When an issue is focused (from the Conflicts page), jump the view to its
// primary entity so the affected blocks are on screen.
function applyFocus() {
  const f = focused.value
  if (!f?.primary) return
  const kind = f.primary.kind
  const nextMode =
    kind === 'ENTITY_KIND_TEACHER'
      ? 'teacher'
      : kind === 'ENTITY_KIND_ROOM'
        ? 'room'
        : 'division'
  mode.value = nextMode
  nextTick(() => {
    if (entityItems.value.some((i) => i.value === f.primary!.id)) {
      entityId.value = f.primary!.id
    }
  })
}
watch(focused, applyFocus)

const highlightLessons = computed(() =>
  (focused.value?.lessons ?? []).map((l) => l.lessonId),
)
const highlightSpans = computed(() => focused.value?.spans ?? [])
const focusLabel = computed(() => {
  const f = focused.value
  if (!f) return ''
  const who = f.primary ? entityRefName(f.primary, model.value) : ''
  return who ? `${f.label} · ${who}` : f.label
})

function onLessonClick(lessonId: number) {
  selectedLessonId.value = lessonId
  dialogOpen.value = true
}

async function onResolve() {
  edited.value = false
  await resolveCurrent({ maxTimeSeconds: 30, numWorkers: 8, randomSeed: 7 })
}
</script>

<template>
  <v-card>
    <v-toolbar density="comfortable" color="transparent">
      <v-toolbar-title>Schedule</v-toolbar-title>
      <v-spacer />
      <div class="d-flex ga-2 align-center">
        <v-btn
          v-if="edited"
          color="primary"
          prepend-icon="mdi-refresh"
          :loading="starting"
          @click="onResolve"
        >
          Re-solve with changes
        </v-btn>
        <v-btn-toggle v-model="mode" mandatory density="compact" variant="outlined">
          <v-btn value="division">Divisions</v-btn>
          <v-btn value="teacher">Teachers</v-btn>
          <v-btn value="room">Rooms</v-btn>
        </v-btn-toggle>
        <v-select
          v-model="entityId"
          :items="entityItems"
          style="min-width: 220px"
          density="compact"
          hide-details
          label="Show"
        />
      </div>
    </v-toolbar>

    <!-- Focused-issue banner -->
    <v-alert
      v-if="focused"
      :type="focused.severity === 'hard' ? 'error' : 'warning'"
      variant="tonal"
      class="ma-3"
      density="comfortable"
    >
      <div class="d-flex align-center ga-3">
        <v-icon>mdi-crosshairs-gps</v-icon>
        <span>Highlighting <strong>{{ focusLabel }}</strong> — affected blocks are outlined, everything else dimmed.</span>
        <v-spacer />
        <v-btn size="small" variant="text" @click="focusIssue(null)">Clear</v-btn>
      </div>
    </v-alert>

    <v-card-text>
      <div v-if="!model" class="text-medium-emphasis">
        No model loaded — start a solve first.
      </div>
      <div v-else-if="!snapshot" class="text-medium-emphasis">
        Waiting for the first solution…
      </div>
      <ScheduleGrid
        v-else-if="entityId != null"
        :model="model"
        :snapshot="snapshot"
        :mode="mode"
        :entity-id="entityId"
        :highlight-lessons="highlightLessons"
        :highlight-spans="highlightSpans"
        :highlight-severity="focused?.severity ?? null"
        :dim-others="!!focused"
        @lesson-click="onLessonClick"
      />
    </v-card-text>
  </v-card>
  <ScheduleLessonDialog
    v-if="model"
    v-model="dialogOpen"
    :model="model"
    :lesson-id="selectedLessonId"
    @changed="edited = true"
  />
</template>
