<!-- app/components/solve/ImportCard.vue -->

<script setup lang="ts">
import type { ImportResponse, ImportSummary } from '~/composables/useArrangoApi'

const api = useArrangoApi()
const { loadImported } = useSolveJob()

const file = ref<File | null>(null)
const uploading = ref(false)
const error = ref<string | null>(null)
const summary = ref<ImportSummary | null>(null)
const warnings = ref<string[]>([])
const showWarnings = ref(false)
const valid = ref<boolean | null>(null)

async function onImport() {
  if (!file.value || uploading.value) return
  uploading.value = true
  error.value = null
  try {
    const result: ImportResponse = await api.importArchive(file.value)
    summary.value = result.summary
    warnings.value = result.warnings ?? []
    valid.value = result.validation?.valid ?? null
    await loadImported(result)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    summary.value = null
    valid.value = null
  } finally {
    uploading.value = false
  }
}

const summaryChips = computed(() => {
  if (!summary.value) return []
  const s = summary.value
  return [
    `${s.divisions} divisions`,
    `${s.groups} groups`,
    `${s.teachers} teachers`,
    `${s.rooms} rooms`,
    `${s.subjects} subjects`,
    `${s.lessons} lessons`,
    `${s.days} days × ${s.periods} periods`,
  ]
})
</script>

<template>
  <v-card title="Import Optivum export">
    <v-card-text>
      <div class="d-flex ga-3 align-center flex-wrap">
        <v-file-input
          v-model="file"
          accept=".zip"
          label="Export archive (.zip of the o*/n*/s* HTML pages)"
          prepend-icon="mdi-folder-zip-outline"
          style="max-width: 480px"
        />
        <v-btn
          color="primary"
          prepend-icon="mdi-upload"
          :loading="uploading"
          :disabled="!file"
          @click="onImport"
        >
          Import
        </v-btn>
      </div>
      <div v-if="error" class="text-error mt-2">{{ error }}</div>
      <div v-if="summary" class="mt-3 d-flex ga-2 flex-wrap align-center">
        <v-chip v-for="chip in summaryChips" :key="chip" variant="tonal">
          {{ chip }}
        </v-chip>
        <v-chip
          v-if="valid !== null"
          :color="valid ? 'success' : 'error'"
        >
          {{ valid ? 'imported timetable valid' : 'imported timetable has conflicts' }}
        </v-chip>
      </div>
      <template v-if="warnings.length">
        <v-btn
          variant="text"
          size="small"
          class="mt-2"
          @click="showWarnings = !showWarnings"
        >
          {{ warnings.length }} parser warnings
          <v-icon end>
            {{ showWarnings ? 'mdi-chevron-up' : 'mdi-chevron-down' }}
          </v-icon>
        </v-btn>
        <v-list v-if="showWarnings" density="compact" class="mt-1">
          <v-list-item
            v-for="(warning, i) in warnings"
            :key="i"
            :title="warning"
          />
        </v-list>
      </template>
      <div v-if="summary" class="mt-2 text-medium-emphasis text-body-2">
        The imported timetable is now shown on the Schedule, Conflicts, and
        Scores pages. Hitting Solve re-solves it; imported placements act as
        previous placements, so the stability preference keeps changes small.
      </div>
    </v-card-text>
  </v-card>
</template>
