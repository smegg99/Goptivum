<!-- app/components/data/SubjectRoomRules.vue -->

<script setup lang="ts">
import { deriveSubjectRule } from '~/utils/roomRules'

const ws = useWorkspace()

const designators = computed(() => {
  const values = ws.model.value.rooms
    .map((room) => room.designator)
    .filter(Boolean)
  return [...new Set(values)].sort()
})

function ruleFor(subjectId: number) {
  return ws.rules.value[subjectId] ?? { allowed: [], disallowed: [] }
}

function setAllowed(subjectId: number, allowed: string[]) {
  ws.setSubjectRule(subjectId, { ...ruleFor(subjectId), allowed })
}

function setDisallowed(subjectId: number, disallowed: string[]) {
  ws.setSubjectRule(subjectId, { ...ruleFor(subjectId), disallowed })
}

function seed(subjectId: number) {
  ws.setSubjectRule(subjectId, deriveSubjectRule(ws.model.value, subjectId))
}
</script>

<template>
  <v-card>
    <v-card-title class="text-subtitle-1">
      Per-subject room rules
    </v-card-title>
    <v-card-text>
      <v-alert
        v-if="!designators.length"
        type="info"
        class="mb-3"
      >
        Add rooms first. Rules use room designators.
      </v-alert>

      <div
        v-if="!ws.model.value.subjects.length"
        class="text-body-2 text-medium-emphasis"
      >
        Add subjects to configure room rules.
      </div>

      <div
        v-for="subject in ws.model.value.subjects"
        :key="subject.id"
        class="subject-rule-row"
      >
        <div class="subject-rule-row__name">
          <div class="text-body-2 font-weight-medium">
            {{ subject.name }}
          </div>
          <div class="text-caption text-medium-emphasis">
            #{{ subject.id }}
          </div>
        </div>

        <v-select
          :model-value="ruleFor(subject.id).allowed"
          :items="designators"
          label="Allowed"
          multiple
          chips
          closable-chips
          @update:model-value="setAllowed(subject.id, $event)"
        />
        <v-select
          :model-value="ruleFor(subject.id).disallowed"
          :items="designators"
          label="Disallowed"
          multiple
          chips
          closable-chips
          @update:model-value="setDisallowed(subject.id, $event)"
        />
        <v-btn
          variant="text"
          size="small"
          prepend-icon="mdi-refresh"
          @click="seed(subject.id)"
        >
          Seed
        </v-btn>
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.subject-rule-row {
  display: grid;
  grid-template-columns: minmax(140px, 200px) minmax(180px, 1fr) minmax(180px, 1fr) auto;
  gap: 12px;
  align-items: start;
  margin-bottom: 12px;
}

.subject-rule-row__name {
  min-width: 0;
  padding-top: 8px;
}

@media (max-width: 900px) {
  .subject-rule-row {
    grid-template-columns: 1fr;
  }
}
</style>
