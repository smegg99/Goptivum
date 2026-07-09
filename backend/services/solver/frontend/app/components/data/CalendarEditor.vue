<!-- app/components/data/CalendarEditor.vue -->

<script setup lang="ts">
import { deepClone, nextId } from '~/utils/model'

const ws = useWorkspace()

function updateDayName(dayId: number, name: string) {
  const next = deepClone(ws.model.value)
  const day = next.days.find((item) => item.id === dayId)
  if (day) day.name = name
  ws.model.value = next
  ws.persist()
}

function updateDayPeriods(dayId: number, value: number) {
  const next = deepClone(ws.model.value)
  const day = next.days.find((item) => item.id === dayId)
  if (day) day.periodCount = Math.max(1, Math.floor(value || 1))

  const maxPeriods = Math.max(1, ...next.days.map((item) => item.periodCount))
  while (next.periods.length < maxPeriods) {
    next.periods.push({ id: nextId(next), name: String(next.periods.length + 1) })
  }
  next.periods = next.periods.slice(0, maxPeriods)

  ws.model.value = next
  ws.persist()
}
</script>

<template>
  <v-card max-width="760">
    <v-card-title class="text-subtitle-1">
      Calendar
    </v-card-title>
    <v-card-text>
      <div
        v-for="day in ws.model.value.days"
        :key="day.id"
        class="calendar-row"
      >
        <v-text-field
          :model-value="day.name"
          label="Day"
          @update:model-value="updateDayName(day.id, String($event))"
        />
        <v-text-field
          :model-value="day.periodCount"
          label="Periods"
          type="number"
          @update:model-value="updateDayPeriods(day.id, Number($event))"
        />
      </div>
      <div class="text-caption text-medium-emphasis">
        {{ ws.model.value.periods.length }} global period slots
      </div>
    </v-card-text>
  </v-card>
</template>

<style scoped>
.calendar-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 140px;
  gap: 12px;
  align-items: start;
}

@media (max-width: 620px) {
  .calendar-row {
    grid-template-columns: 1fr;
  }
}
</style>
