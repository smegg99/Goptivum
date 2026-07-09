<!-- app/components/data/WorkspaceToolbar.vue -->

<script setup lang="ts">
const ws = useWorkspace()

const busy = ref(false)
const error = defineModel<string>('error', { required: true })
const fileInput = ref<HTMLInputElement | null>(null)
const presetChoice = ref('DEMO_PRESET_PRODUCTION')

const presets = [
  { value: 'DEMO_PRESET_PRODUCTION', title: 'Production' },
  { value: 'DEMO_PRESET_MEGA', title: 'Mega' },
]

async function onLoadPreset() {
  busy.value = true
  error.value = ''
  try {
    await ws.loadPreset(presetChoice.value)
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : String(caught)
  } finally {
    busy.value = false
  }
}

function onExport() {
  const blob = new Blob([ws.exportJson()], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'arrango-workspace.json'
  link.click()
  URL.revokeObjectURL(url)
}

async function onImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  try {
    ws.importJson(await file.text())
    error.value = ''
  } catch (caught) {
    error.value = caught instanceof Error ? caught.message : String(caught)
  } finally {
    input.value = ''
  }
}
</script>

<template>
  <div class="data-toolbar">
    <v-select
      v-model="presetChoice"
      :items="presets"
      label="Preset"
      class="data-toolbar__preset"
    />
    <v-btn
      :loading="busy"
      prepend-icon="mdi-download"
      @click="onLoadPreset"
    >
      Load
    </v-btn>
    <v-btn
      variant="text"
      prepend-icon="mdi-file-plus"
      @click="ws.loadEmpty()"
    >
      New
    </v-btn>
    <v-btn
      variant="text"
      prepend-icon="mdi-upload"
      @click="fileInput?.click()"
    >
      Import
    </v-btn>
    <v-btn
      variant="text"
      prepend-icon="mdi-content-save"
      @click="onExport"
    >
      Export
    </v-btn>
    <input
      ref="fileInput"
      type="file"
      accept="application/json"
      hidden
      @change="onImportFile"
    >
  </div>
</template>

<style scoped>
.data-toolbar {
  display: flex;
  align-items: start;
  gap: 8px;
  flex-wrap: wrap;
  justify-content: flex-end;
}

.data-toolbar__preset {
  width: 180px;
}
</style>
