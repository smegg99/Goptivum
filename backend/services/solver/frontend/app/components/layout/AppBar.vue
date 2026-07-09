<!-- app/components/layout/AppBar.vue -->

<script setup lang="ts">
const { job } = useSolveJob()

const stateColor = computed(() => {
  switch (job.value.state) {
    case 'running':
      return 'primary'
    case 'done':
      return 'success'
    case 'cancelled':
      return 'warning'
    case 'error':
      return 'error'
    default:
      return undefined
  }
})
</script>

<template>
  <v-app-bar
    flat
    height="64"
    class="app-bar-border"
  >
    <div class="d-flex align-center px-4 ga-3">
      <v-icon
        icon="mdi-timetable"
        color="primary"
        size="28"
      />
      <div>
        <div class="text-subtitle-1 font-weight-bold">
          Arrango
        </div>
        <div class="text-caption text-medium-emphasis">
          School timetable workspace
        </div>
      </div>
    </div>

    <v-spacer />

    <v-chip
      v-if="job.state"
      :color="stateColor"
      class="mr-4"
    >
      {{ job.state }}
    </v-chip>
  </v-app-bar>
</template>
