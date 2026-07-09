<!-- app/components/scores/ScoreTable.vue -->

<script setup lang="ts">
import type { EntityScore } from '~/types/arrango'

defineProps<{
  title: string
  scores: EntityScore[]
}>()

function qualityColor(quality: number): string {
  if (quality >= 80) return 'success'
  if (quality >= 55) return 'warning'
  return 'error'
}
</script>

<template>
  <v-card :title="title">
    <v-card-text>
      <v-table density="compact">
        <thead>
          <tr>
            <th>Name</th>
            <th class="text-right">Quality</th>
            <th class="text-right">Penalty</th>
            <th>Breakdown</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="score in scores" :key="score.entityId">
            <td>{{ score.name }}</td>
            <td class="text-right">
              <v-chip :color="qualityColor(score.quality)" variant="tonal">
                {{ score.quality.toFixed(1) }}
              </v-chip>
            </td>
            <td class="text-right">{{ score.penalty ?? '0' }}</td>
            <td>
              <v-chip
                v-for="item in score.items ?? []"
                :key="item.category"
                class="mr-1 my-1"
                variant="tonal"
              >
                {{ item.category }}: {{ item.penalty }}
              </v-chip>
            </td>
          </tr>
        </tbody>
      </v-table>
    </v-card-text>
  </v-card>
</template>
