<!-- app/pages/conflicts.vue -->

<script setup lang="ts">
import type { SchoolModel, LessonRef, TimeSpan } from '~/types/arrango'
import {
  entityKindMeta,
  entityRefName,
  type Issue,
} from '~/composables/useIssues'

const { job } = useSolveJob()
const { hardIssues, softIssues, focusIssue } = useIssues()

const model = computed(() => job.value.model)
const lookup = useSchoolLookup(model as Ref<SchoolModel | null>)
const validation = computed(() => job.value.latest?.validation ?? null)

const dayName = (dayId?: number) =>
  dayId ? (lookup.days.value.get(dayId)?.name ?? `day ${dayId}`) : ''

function spanLabel(s: TimeSpan): string {
  const day = dayName(s.dayId)
  const from = s.startPeriod + 1
  const to = s.startPeriod + Math.max(s.periodSpan, 1)
  return s.periodSpan > 1 ? `${day} · periods ${from}–${to}` : `${day} · period ${from}`
}

function lessonLabel(l: LessonRef): string {
  const base = lookup.lessonLabel(l.lessonId)
  if (l.dayId && l.startPeriod != null) {
    return `${base} — ${dayName(l.dayId)} p${l.startPeriod + 1}`
  }
  return `${base} — unplaced`
}

// Soft issues grouped by (primary entity, category), worst first.
interface Group {
  key: string
  label: string
  entityName: string
  penalty: number
  count: number
  issues: Issue[]
}
const softGroups = computed<Group[]>(() => {
  const groups = new Map<string, Group>()
  for (const issue of softIssues.value) {
    const name = issue.primary
      ? entityRefName(issue.primary, model.value)
      : issue.entities[0]
        ? entityRefName(issue.entities[0], model.value)
        : '—'
    const key = `${name}|${issue.code}`
    let g = groups.get(key)
    if (!g) {
      g = { key, label: issue.label, entityName: name, penalty: 0, count: 0, issues: [] }
      groups.set(key, g)
    }
    g.penalty += issue.penalty
    g.count += issue.count
    g.issues.push(issue)
  }
  return [...groups.values()].sort((a, b) => b.penalty - a.penalty)
})

async function locate(issue: Issue) {
  focusIssue(issue)
  await navigateTo('/schedule')
}
</script>

<template>
  <div class="issues">
    <!-- Summary strip -->
    <div class="summary">
      <div class="stat stat--hard">
        <span class="stat__n">{{ hardIssues.length }}</span>
        <span class="stat__l">hard conflicts</span>
      </div>
      <div class="stat stat--soft">
        <span class="stat__n">{{ softIssues.length }}</span>
        <span class="stat__l">soft issues</span>
      </div>
      <div class="stat stat--ok" v-if="validation?.valid">
        <v-icon size="small">mdi-check-circle</v-icon>
        <span class="stat__l">schedule is hard-valid</span>
      </div>
    </div>

    <!-- Hard conflicts -->
    <section class="block">
      <h2 class="block__title">Hard conflicts</h2>
      <p v-if="!validation" class="muted">No validation yet — run a solve.</p>
      <p v-else-if="!hardIssues.length" class="muted">
        None. Every hard constraint holds.
      </p>
      <ul v-else class="rows">
        <li v-for="issue in hardIssues" :key="issue.id" class="row row--hard">
          <div class="row__head">
            <span class="badge badge--hard">{{ issue.label }}</span>
            <span
              v-for="(e, i) in issue.entities"
              :key="i"
              class="chip chip--entity"
            >
              <v-icon size="x-small">{{ entityKindMeta(e.kind).icon }}</v-icon>
              {{ entityRefName(e, model) }}
            </span>
            <span v-for="(s, i) in issue.spans" :key="`s${i}`" class="chip chip--time">
              {{ spanLabel(s) }}
            </span>
            <v-spacer />
            <button
              v-if="issue.lessons.length || issue.spans.length"
              class="locate"
              @click="locate(issue)"
            >
              <v-icon size="x-small">mdi-crosshairs-gps</v-icon> Locate
            </button>
          </div>
          <div v-if="issue.lessons.length" class="row__lessons">
            <span
              v-for="(l, i) in issue.lessons"
              :key="`l${i}`"
              class="chip chip--lesson"
            >
              {{ lessonLabel(l) }}
            </span>
          </div>
        </li>
      </ul>
    </section>

    <!-- Soft issues -->
    <section class="block">
      <h2 class="block__title">Soft issues</h2>
      <p v-if="!softIssues.length" class="muted">
        {{ job.latest ? 'None — every preference is satisfied.' : 'No score yet — run a solve.' }}
      </p>
      <v-expansion-panels v-else variant="accordion" class="soft">
        <v-expansion-panel v-for="g in softGroups" :key="g.key">
          <v-expansion-panel-title>
            <div class="soft__head">
              <span class="badge badge--soft">{{ g.label }}</span>
              <strong>{{ g.entityName }}</strong>
              <span class="chip">penalty {{ g.penalty.toLocaleString() }}</span>
              <span class="chip">{{ g.count }}×</span>
            </div>
          </v-expansion-panel-title>
          <v-expansion-panel-text>
            <ul class="rows">
              <li v-for="issue in g.issues" :key="issue.id" class="row">
                <div class="row__head">
                  <span
                    v-for="(s, i) in issue.spans"
                    :key="i"
                    class="chip chip--time"
                  >
                    {{ spanLabel(s) }}
                  </span>
                  <span class="chip">{{ issue.count }}×</span>
                  <span class="chip">{{ issue.penalty.toLocaleString() }}</span>
                  <v-spacer />
                  <button
                    v-if="issue.lessons.length || issue.spans.length"
                    class="locate"
                    @click="locate(issue)"
                  >
                    <v-icon size="x-small">mdi-crosshairs-gps</v-icon> Locate
                  </button>
                </div>
                <div v-if="issue.lessons.length" class="row__lessons">
                  <span
                    v-for="(l, i) in issue.lessons"
                    :key="i"
                    class="chip chip--lesson"
                  >
                    {{ lessonLabel(l) }}
                  </span>
                </div>
              </li>
            </ul>
          </v-expansion-panel-text>
        </v-expansion-panel>
      </v-expansion-panels>
    </section>
  </div>
</template>

<style scoped>
.issues {
  display: flex;
  flex-direction: column;
  gap: 20px;
}
.summary {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.stat {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 10px 16px;
  border-radius: 10px;
  border: 1px solid rgba(128, 128, 128, 0.25);
}
.stat__n {
  font-size: 1.5rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.stat__l {
  font-size: 0.8rem;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  opacity: 0.7;
}
.stat--hard {
  border-color: rgb(var(--v-theme-error));
}
.stat--hard .stat__n {
  color: rgb(var(--v-theme-error));
}
.stat--soft .stat__n {
  color: rgb(var(--v-theme-warning));
}
.stat--ok {
  color: rgb(var(--v-theme-success));
  align-items: center;
}
.block__title {
  font-size: 0.85rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  opacity: 0.7;
  margin-bottom: 10px;
}
.muted {
  opacity: 0.6;
}
.rows {
  list-style: none;
  padding: 0;
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.row {
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(128, 128, 128, 0.06);
  border-left: 3px solid transparent;
}
.row--hard {
  border-left-color: rgb(var(--v-theme-error));
}
.row__head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.row__lessons {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 8px;
  padding-left: 4px;
}
.badge {
  font-weight: 600;
  font-size: 0.85rem;
  padding: 2px 10px;
  border-radius: 6px;
}
.badge--hard {
  background: rgba(var(--v-theme-error), 0.15);
  color: rgb(var(--v-theme-error));
}
.badge--soft {
  background: rgba(var(--v-theme-warning), 0.18);
  color: rgb(var(--v-theme-warning));
}
.chip {
  font-size: 0.78rem;
  padding: 2px 8px;
  border-radius: 6px;
  background: rgba(128, 128, 128, 0.14);
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-variant-numeric: tabular-nums;
}
.chip--entity {
  background: rgba(128, 128, 128, 0.2);
}
.chip--time {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
}
.chip--lesson {
  font-family: 'JetBrains Mono', ui-monospace, monospace;
  background: rgba(128, 128, 128, 0.1);
}
.locate {
  font-size: 0.78rem;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 6px;
  border: 1px solid rgba(128, 128, 128, 0.3);
  cursor: pointer;
  background: transparent;
  color: inherit;
}
.locate:hover {
  border-color: rgb(var(--v-theme-primary));
  color: rgb(var(--v-theme-primary));
}
.soft__head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
</style>
