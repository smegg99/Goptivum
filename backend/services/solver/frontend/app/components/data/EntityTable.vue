<!-- app/components/data/EntityTable.vue -->

<script setup lang="ts">
import type { EntityKind } from '~/utils/entityOps'
import { ENTITY_LABELS, FIELD_SPECS, entityCollection } from '~/utils/entityFields'

const props = defineProps<{ kind: EntityKind }>()
const ws = useWorkspace()

const dialogOpen = ref(false)
const editId = ref<number | null>(null)
const removeError = ref('')

const rows = computed(() => (
  ws.model.value[entityCollection(props.kind)] as Array<Record<string, unknown>>
))
const headers = computed(() => [
  { title: 'ID', key: 'id', width: 88 },
  ...FIELD_SPECS[props.kind].map((field) => ({ title: field.label, key: field.key })),
  { title: '', key: 'actions', sortable: false, align: 'end' as const },
])
const label = computed(() => ENTITY_LABELS[props.kind])

function onNew() {
  editId.value = null
  dialogOpen.value = true
}

function onEdit(id: number) {
  editId.value = id
  dialogOpen.value = true
}

function onRemove(id: number) {
  const result = ws.removeEntity(props.kind, id)
  removeError.value = result.ok ? '' : `Cannot delete: ${result.blockers.join('; ')}`
}
</script>

<template>
  <div>
    <div class="d-flex align-center ga-3 mb-3">
      <div>
        <div class="text-subtitle-1 text-capitalize font-weight-medium">
          {{ label.plural }}
        </div>
        <div class="text-caption text-medium-emphasis">
          {{ rows.length }} records
        </div>
      </div>
      <v-spacer />
      <v-btn
        color="primary"
        prepend-icon="mdi-plus"
        @click="onNew"
      >
        Add {{ label.singular }}
      </v-btn>
    </div>

    <v-alert
      v-if="removeError"
      type="warning"
      class="mb-3"
      closable
      @click:close="removeError = ''"
    >
      {{ removeError }}
    </v-alert>

    <v-data-table
      :headers="headers"
      :items="rows"
      :items-per-page="25"
    >
      <template #[`item.actions`]="{ item }">
        <div class="d-flex justify-end ga-1">
          <v-btn
            icon="mdi-pencil"
            size="small"
            variant="text"
            aria-label="Edit"
            @click="onEdit((item as { id: number }).id)"
          />
          <v-btn
            icon="mdi-delete"
            size="small"
            variant="text"
            color="error"
            aria-label="Delete"
            @click="onRemove((item as { id: number }).id)"
          />
        </div>
      </template>
    </v-data-table>

    <DataEntityFormDialog
      v-model="dialogOpen"
      :kind="kind"
      :edit-id="editId"
    />
  </div>
</template>
