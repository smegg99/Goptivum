<!-- app/components/solve/SolverConfigPanel.vue -->

<script setup lang="ts">
import {
  useSolverConfig,
  SOLVE_MODES,
  CAPACITY_POLICIES,
  UNPLACED_POLICIES,
  POOL_STRATEGIES,
  CONFIG_PRESETS,
} from '~/composables/useSolverConfig'

const { config, applyPreset, reset } = useSolverConfig()
const presetNames = Object.keys(CONFIG_PRESETS)

// Objective weight fields (int64 arrive/return as strings).
const weightFields: { key: keyof NonNullable<typeof config.value.weights>; label: string }[] = [
  { key: 'studentGapBase', label: 'Student gap' },
  { key: 'teacherGapBase', label: 'Teacher gap' },
  { key: 'lateStudentLessonBase', label: 'Late (students)' },
  { key: 'lateTeacherFinishBase', label: 'Late (teacher)' },
  { key: 'subjectSplitBase', label: 'Subject split' },
  { key: 'blockBreakBase', label: 'Block break' },
  { key: 'roomChangeBase', label: 'Room change' },
  { key: 'stabilityMoveBase', label: 'Moved from previous' },
]
</script>

<template>
  <div class="cfg">
    <div class="cfg__presets">
      <span class="cfg__label">Preset</span>
      <v-chip
        v-for="name in presetNames"
        :key="name"
        size="small"
        variant="outlined"
        @click="applyPreset(name)"
      >
        {{ name }}
      </v-chip>
      <v-spacer />
      <v-btn size="small" variant="text" prepend-icon="mdi-restore" @click="reset">
        Reset
      </v-btn>
    </div>

    <v-expansion-panels multiple variant="accordion" :model-value="[0]">
      <!-- Run -->
      <v-expansion-panel title="Run">
        <template #text>
          <v-row dense>
            <v-col cols="12" md="4">
              <v-select
                v-model="config.mode"
                :items="SOLVE_MODES"
                label="Mode"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="3">
              <v-text-field
                v-model.number="config.totalTimeSeconds"
                type="number"
                label="Total time (s)"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="2">
              <v-text-field
                v-model.number="config.numWorkers"
                type="number"
                label="Workers (0=all)"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="3">
              <v-text-field
                v-model.number="config.randomSeed"
                type="number"
                label="Seed"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="12">
              <v-switch
                v-model="config.ignorePreviousPlacements"
                color="primary"
                density="compact"
                hide-details
                label="From scratch (ignore previous placements & baseline)"
              />
            </v-col>
          </v-row>
        </template>
      </v-expansion-panel>

      <!-- Rooms & placement -->
      <v-expansion-panel title="Rooms & placement">
        <template #text>
          <v-row dense>
            <v-col cols="12" md="6">
              <v-select
                v-model="config.unknownCapacityPolicy"
                :items="CAPACITY_POLICIES"
                label="Unknown-capacity rooms"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="12" md="6">
              <v-select
                v-model="config.unplacedPolicy"
                :items="UNPLACED_POLICIES"
                label="Unplaced lessons"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="4">
              <v-text-field
                v-model.number="config.exactRoomThreshold"
                type="number"
                label="Exact-room threshold"
                density="compact"
                hide-details
              />
            </v-col>
          </v-row>
        </template>
      </v-expansion-panel>

      <!-- Staged pipeline (future) -->
      <v-expansion-panel>
        <v-expansion-panel-title>
          Pooling &amp; repair
          <v-chip size="x-small" class="ml-2" variant="tonal">staged pipeline</v-chip>
        </v-expansion-panel-title>
        <v-expansion-panel-text>
          <p class="text-caption text-medium-emphasis mb-2">
            Reserved for the staged construct → repair → polish pipeline (not
            implemented yet). Carried and recorded with every run today.
          </p>
          <v-row dense>
            <v-col cols="12" md="4">
              <v-select
                v-model="config.poolStrategy"
                :items="POOL_STRATEGIES"
                label="Room-pool strategy"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="2">
              <v-text-field
                v-model.number="config.portfolioSeeds"
                type="number"
                label="Portfolio seeds"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="6" md="3">
              <v-text-field
                v-model.number="config.lnsIterations"
                type="number"
                label="LNS iterations"
                density="compact"
                hide-details
              />
            </v-col>
            <v-col cols="12" md="3">
              <v-switch
                v-model="config.disableLns"
                color="primary"
                density="compact"
                hide-details
                label="Disable LNS repair"
              />
            </v-col>
          </v-row>
        </v-expansion-panel-text>
      </v-expansion-panel>

      <!-- Objective weights -->
      <v-expansion-panel title="Objective weights">
        <template #text>
          <p class="text-caption text-medium-emphasis mb-2">
            Higher = the solver tries harder to avoid it. Student gaps are
            near-hard by default.
          </p>
          <v-row dense>
            <v-col
              v-for="f in weightFields"
              :key="f.key"
              cols="6"
              md="3"
            >
              <v-text-field
                v-model="config.weights![f.key]"
                :label="f.label"
                density="compact"
                hide-details
              />
            </v-col>
          </v-row>
        </template>
      </v-expansion-panel>
    </v-expansion-panels>
  </div>
</template>

<style scoped>
.cfg {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.cfg__presets {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.cfg__label {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  opacity: 0.6;
}
</style>
