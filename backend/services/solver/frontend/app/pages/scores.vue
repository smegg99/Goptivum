<!-- app/pages/scores.vue -->

<script setup lang="ts">
const { job } = useSolveJob()

const score = computed(() => job.value.latest?.score ?? null)
</script>

<template>
  <div v-if="!score" class="text-medium-emphasis">
    No score report yet — start a solve first.
  </div>
  <v-row v-else>
    <v-col cols="12">
      <v-card title="Overall">
        <v-card-text class="d-flex ga-3 flex-wrap">
          <v-chip color="primary" size="large">
            overall {{ score.overallQuality.toFixed(1) }}
          </v-chip>
          <v-chip size="large">
            students {{ score.allStudentsQuality.toFixed(1) }}
          </v-chip>
          <v-chip size="large">
            teachers {{ score.allTeachersQuality.toFixed(1) }}
          </v-chip>
          <v-chip size="large">penalty {{ score.totalPenalty ?? '0' }}</v-chip>
          <v-chip
            v-for="item in score.globalItems ?? []"
            :key="item.category"
            variant="tonal"
          >
            {{ item.category }}: {{ item.penalty }} ({{ item.count }}x)
          </v-chip>
        </v-card-text>
      </v-card>
    </v-col>
    <v-col cols="12" lg="6">
      <ScoresScoreTable title="Divisions" :scores="score.divisionScores ?? []" />
    </v-col>
    <v-col cols="12" lg="6">
      <ScoresScoreTable title="Teachers" :scores="score.teacherScores ?? []" />
    </v-col>
    <v-col cols="12" lg="6">
      <ScoresScoreTable title="Years" :scores="score.yearScores ?? []" />
    </v-col>
  </v-row>
</template>
