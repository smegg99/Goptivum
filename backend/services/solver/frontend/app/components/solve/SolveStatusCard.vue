<!-- app/components/solve/SolveStatusCard.vue -->

<script setup lang="ts">
const { job } = useSolveJob()

const latest = computed(() => job.value.latest)
const progress = computed(() => job.value.progress ?? job.value.latest)

const statusText = computed(() => {
  const update = latest.value
  if (!update) return job.value.state ? `job ${job.value.state}` : 'no job yet'
  const status = update.status?.replace('SOLVE_STATUS_', '') ?? ''
  const phase = update.phase?.replace('SOLVE_PHASE_', '') ?? ''
  return `${phase}${status ? ` · ${status}` : ''}`
})

// Live pipeline stage from SolveProgress; LNS shows its counters inline.
const stageText = computed(() => {
  const p = progress.value?.progress
  if (!p?.stage || p.stage === 'SOLVE_STAGE_UNSPECIFIED') return null
  if (p.stage === 'SOLVE_STAGE_LNS') {
    return `LNS pass ${p.lnsPass ?? 1} · ${p.lnsNeighborhood ?? 0}/${p.lnsNeighborhoodsTotal ?? 0} · ${p.lnsAccepted ?? 0} accepted`
  }
  const name = p.stage.replace('SOLVE_STAGE_', '')
  return p.detail ? `${name} · ${p.detail}` : name
})

// One-glance verdict: pristine / warnings / errors.
const verdict = computed(() => {
  const v = latest.value?.verdict
  if (!v || v.tier === 'TIER_UNSPECIFIED') return null
  if (v.tier === 'TIER_PRISTINE') return { color: 'success', text: 'pristine' }
  if (v.tier === 'TIER_WARNINGS') {
    return { color: 'warning', text: `${v.warnings ?? 0} warnings` }
  }
  return { color: 'error', text: `${v.errors ?? 0} errors` }
})

// Optimality gap from the live heartbeat: objective vs proven bound.
const gapText = computed(() => {
  const p = progress.value
  if (!p) return null
  const objective = Number(p.objective ?? 0)
  const bound = Number(p.bestBound ?? 0)
  if (objective <= 0) return null
  if (bound >= objective) return 'proven optimal'
  const gap = ((objective - bound) / objective) * 100
  return `gap ${gap.toFixed(1)}% (bound ${bound})`
})

const recentHistory = computed(() => job.value.history.slice(-12))
</script>

<template>
  <v-card title="Live status">
    <v-card-text>
      <div v-if="!latest && job.state !== 'running'" class="text-medium-emphasis">
        Start a solve to see live updates.
      </div>
      <template v-else>
        <div class="d-flex align-center ga-4 flex-wrap">
          <v-progress-circular
            v-if="job.state === 'running'"
            indeterminate
            size="20"
            color="primary"
          />
          <span class="text-body-1">{{ statusText }}</span>
          <v-chip v-if="stageText" variant="tonal" color="primary">
            {{ stageText }}
          </v-chip>
          <v-chip v-if="verdict" :color="verdict.color">
            {{ verdict.text }}
          </v-chip>
          <v-chip v-if="latest?.score" color="primary">
            quality {{ latest.score.overallQuality.toFixed(1) }}
          </v-chip>
          <v-chip v-if="progress">
            objective {{ progress.objective ?? '0' }}
          </v-chip>
          <v-chip v-if="gapText" variant="tonal">{{ gapText }}</v-chip>
          <v-chip v-if="progress">
            {{ progress.solutionsFound ?? 0 }} solutions
          </v-chip>
          <v-chip v-if="progress">
            {{ (progress.wallTimeSeconds ?? 0).toFixed(0) }}s
          </v-chip>
          <v-chip
            v-if="latest?.validation"
            :color="latest.validation.valid ? 'success' : 'error'"
          >
            {{
              latest.validation.valid
                ? 'valid'
                : `${latest.validation.conflicts?.length ?? 0} conflicts`
            }}
          </v-chip>
        </div>
        <div v-if="recentHistory.length > 1" class="mt-4 d-flex ga-1 flex-wrap">
          <v-chip
            v-for="(point, i) in recentHistory"
            :key="i"
            variant="tonal"
            :color="i === recentHistory.length - 1 ? 'primary' : undefined"
          >
            {{ point.overallQuality.toFixed(1) }}
          </v-chip>
        </div>
        <div v-if="latest?.message" class="mt-2 text-medium-emphasis">
          {{ latest.message }}
        </div>
      </template>
    </v-card-text>
  </v-card>
</template>
