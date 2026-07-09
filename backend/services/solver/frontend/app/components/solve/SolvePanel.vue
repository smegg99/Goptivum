<!-- app/components/solve/SolvePanel.vue -->

<script setup lang="ts">
const { job, starting, cancelling, start, resolveCurrent, cancel } =
  useSolveJob()
const { config } = useSolverConfig()
const api = useArrangoApi()
const ws = useWorkspace()

const presets = [
  { value: 'DEMO_PRESET_PRODUCTION', title: 'Production (34 divisions, ~1200 lessons)' },
  { value: 'DEMO_PRESET_MEGA', title: 'Mega (46 divisions, every feature)' },
]

const preset = ref('DEMO_PRESET_PRODUCTION')
const source = ref<'workspace' | 'preset'>('workspace')
const seed = ref(42)

// Params mirror the config for the legacy code path; the backend uses the
// full config when present.
const params = computed(() => ({
  maxTimeSeconds: config.value.totalTimeSeconds ?? 180,
  numWorkers: config.value.numWorkers ?? 0,
  randomSeed: config.value.randomSeed ?? 7,
  fromScratch: config.value.ignorePreviousPlacements ?? false,
}))

const running = computed(() => job.value.state === 'running')
const hasModel = computed(() => !!job.value.model)
const hasSnapshot = computed(
  () => !!job.value.model && !!job.value.latest?.snapshot,
)
const workspaceLessons = computed(() => ws.model.value.lessons.length)
const exporting = ref(false)
const exportError = ref<string | null>(null)

onMounted(() => ws.reload())

// Downloads the current schedule as a simplified Optivum-style HTML zip.
async function onExport() {
  if (!job.value.model || !job.value.latest?.snapshot || exporting.value) {
    return
  }
  exporting.value = true
  exportError.value = null
  try {
    const blob = await api.exportArchive(
      job.value.model,
      job.value.latest.snapshot,
    )
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = 'arrango-plan.zip'
    link.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    exportError.value = e instanceof Error ? e.message : String(e)
  } finally {
    exporting.value = false
  }
}

async function onStart() {
  if (source.value === 'workspace') {
    await start({
      model: ws.model.value,
      params: params.value,
      config: config.value,
    })
    return
  }

  await start({
    preset: preset.value,
    seed: seed.value,
    params: params.value,
    config: config.value,
  })
}

// Re-solves the currently loaded model (imported or from the last run);
// current placements become previous placements for stability unless
// "from scratch" ignores them.
async function onResolveCurrent() {
  await resolveCurrent(params.value, config.value)
}
</script>

<template>
  <v-card title="Solver run">
    <v-card-text>
      <v-btn-toggle
        v-model="source"
        mandatory
        class="mb-3"
      >
        <v-btn value="workspace">
          Workspace ({{ workspaceLessons }} lessons)
        </v-btn>
        <v-btn value="preset">
          Preset
        </v-btn>
      </v-btn-toggle>

      <v-row dense class="mb-2">
        <v-col
          v-if="source === 'preset'"
          cols="12"
          md="8"
        >
          <v-select
            v-model="preset"
            :items="presets"
            label="Demo preset (for Solve preset)"
            density="compact"
            hide-details
          />
        </v-col>
        <v-col
          v-if="source === 'preset'"
          cols="12"
          md="4"
        >
          <v-text-field
            v-model.number="seed"
            type="number"
            label="Data seed"
            density="compact"
            hide-details
          />
        </v-col>
      </v-row>
      <SolveSolverConfigPanel />
    </v-card-text>
    <v-card-actions>
      <v-btn
        color="primary"
        prepend-icon="mdi-play"
        :loading="starting"
        :disabled="running"
        @click="onStart"
      >
        Solve {{ source }}
      </v-btn>
      <v-btn
        color="secondary"
        prepend-icon="mdi-replay"
        :loading="starting"
        :disabled="running || !hasModel"
        @click="onResolveCurrent"
      >
        Re-solve current model
      </v-btn>
      <v-btn
        color="error"
        variant="text"
        prepend-icon="mdi-stop"
        :loading="cancelling"
        :disabled="!running"
        @click="cancel"
      >
        Cancel
      </v-btn>
      <v-btn
        variant="text"
        prepend-icon="mdi-download"
        :loading="exporting"
        :disabled="!hasSnapshot"
        @click="onExport"
      >
        Export HTML
      </v-btn>
      <v-spacer />
      <span v-if="job.error || exportError" class="text-error text-body-2">
        {{ job.error || exportError }}
      </span>
    </v-card-actions>
  </v-card>
</template>
