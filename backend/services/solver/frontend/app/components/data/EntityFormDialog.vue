<!-- app/components/data/EntityFormDialog.vue -->

<script setup lang="ts">
import type { EntityKind } from '~/utils/entityOps'
import type { FieldSpec } from '~/utils/entityFields'
import { ENTITY_LABELS, FIELD_SPECS, entityCollection } from '~/utils/entityFields'

const props = defineProps<{
  kind: EntityKind
  editId: number | null
}>()

const open = defineModel<boolean>({ required: true })
const ws = useWorkspace()
const form = reactive<Record<string, unknown>>({})

const specs = computed(() => FIELD_SPECS[props.kind])
const title = computed(() => {
  const action = props.editId == null ? 'New' : 'Edit'
  return `${action} ${ENTITY_LABELS[props.kind].singular}`
})

watch(open, (isOpen) => {
  if (!isOpen) return
  resetForm()
  fillForm()
})

function refItems(refKind: EntityKind) {
  const list = ws.model.value[entityCollection(refKind)] as Array<{ id: number, name?: string }>
  return list.map((entity) => ({
    value: entity.id,
    title: entity.name ? `${entity.name} (${entity.id})` : String(entity.id),
  }))
}

function resetForm() {
  for (const spec of specs.value) {
    form[spec.key] = defaultValue(spec)
  }
}

function fillForm() {
  if (props.editId == null) return

  if (props.kind === 'lesson') {
    const lesson = ws.model.value.lessons.find((item) => item.id === props.editId)
    if (!lesson) return
    form.subjectId = lesson.subjectId || ''
    form.teacherId = lesson.teacherId || ''
    form.divisionId = lesson.participants[0]?.divisionId || ''
    form.groupId = lesson.participants[0]?.groupId || ''
    form.duration = lesson.duration
    form.requiresTeacher = lesson.requiresTeacher
    form.requiresRoom = lesson.requiresRoom
    return
  }

  const list = ws.model.value[entityCollection(props.kind)] as Array<Record<string, unknown>>
  const entity = list.find((item) => item.id === props.editId)
  if (!entity) return
  for (const spec of specs.value) {
    form[spec.key] = entity[spec.key] ?? defaultValue(spec)
  }
}

function save() {
  if (props.kind === 'lesson') {
    saveLesson()
  } else {
    saveEntity()
  }
  open.value = false
}

function saveLesson() {
  const participant = {
    divisionId: Number(form.divisionId),
    groupId: form.groupId ? Number(form.groupId) : undefined,
  }
  const patch = {
    subjectId: Number(form.subjectId),
    teacherId: form.teacherId ? Number(form.teacherId) : 0,
    duration: Math.max(1, Number(form.duration) || 1),
    requiresTeacher: Boolean(form.requiresTeacher),
    requiresRoom: Boolean(form.requiresRoom),
    participants: [participant],
  }

  if (props.editId == null) {
    ws.addLesson(patch)
  } else {
    ws.updateEntity('lesson', props.editId, patch)
  }
}

function saveEntity() {
  const patch = Object.fromEntries(
    specs.value.map((spec) => [spec.key, coerceValue(spec, form[spec.key])]),
  )

  if (props.editId != null) {
    ws.updateEntity(props.kind, props.editId, patch)
    return
  }

  switch (props.kind) {
    case 'year':
      ws.addYear(patch as Parameters<typeof ws.addYear>[0])
      break
    case 'division':
      ws.addDivision(patch as Parameters<typeof ws.addDivision>[0])
      break
    case 'group':
      ws.addGroup(patch as Parameters<typeof ws.addGroup>[0])
      break
    case 'teacher':
      ws.addTeacher(patch as Parameters<typeof ws.addTeacher>[0])
      break
    case 'subject':
      ws.addSubject(patch as Parameters<typeof ws.addSubject>[0])
      break
    case 'room':
      ws.addRoom(patch as Parameters<typeof ws.addRoom>[0])
      break
  }
}

function defaultValue(spec: FieldSpec) {
  if (spec.type === 'boolean') return false
  if (spec.type === 'number') return 1
  return ''
}

function coerceValue(spec: FieldSpec, value: unknown) {
  if (spec.type === 'number') return Number(value) || 0
  if (spec.type === 'boolean') return Boolean(value)
  if (spec.type === 'ref') return value ? Number(value) : 0
  return String(value ?? '').trim()
}
</script>

<template>
  <v-dialog
    v-model="open"
    max-width="560"
  >
    <v-card :title="title">
      <v-card-text>
        <template
          v-for="spec in specs"
          :key="spec.key"
        >
          <v-text-field
            v-if="spec.type === 'text'"
            v-model="form[spec.key]"
            :label="spec.label"
          />
          <v-text-field
            v-else-if="spec.type === 'number'"
            v-model.number="form[spec.key]"
            type="number"
            :label="spec.label"
          />
          <v-switch
            v-else-if="spec.type === 'boolean'"
            v-model="form[spec.key]"
            :label="spec.label"
            color="primary"
          />
          <v-select
            v-else-if="spec.refKind"
            v-model="form[spec.key]"
            :items="refItems(spec.refKind)"
            :label="spec.label"
            :clearable="!spec.required"
          />
        </template>
      </v-card-text>
      <v-card-actions>
        <v-spacer />
        <v-btn
          variant="text"
          @click="open = false"
        >
          Cancel
        </v-btn>
        <v-btn
          color="primary"
          @click="save"
        >
          Save
        </v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>
