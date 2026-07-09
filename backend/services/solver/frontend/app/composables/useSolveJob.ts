// app/composables/useSolveJob.ts

import type { ImportResponse } from '~/composables/useArrangoApi'
import type {
  JobState,
  SchoolModel,
  SolveParams,
  SolveUpdate,
  SolverConfig,
} from '~/types/arrango'

export interface QualityPoint {
  wallTimeSeconds: number
  overallQuality: number
  objective: string
}

interface SolveJobState {
  jobId: string | null
  state: JobState | null
  model: SchoolModel | null
  latest: SolveUpdate | null   // latest update carrying a snapshot
  progress: SolveUpdate | null // latest heartbeat (objective/bound only)
  history: QualityPoint[]
  error: string | null
  // The config snapshot the solver echoed for this run; results are
  // compared by it.
  config: SolverConfig | null
}

// App-wide singleton: the schedule/conflicts/scores pages all render the
// same running job.
export function useSolveJob() {
  const api = useArrangoApi()
  const job = useState<SolveJobState>('solve-job', () => ({
    jobId: null,
    state: null,
    model: null,
    latest: null,
    progress: null,
    history: [],
    error: null,
    config: null,
  }))
  const source = useState<EventSource | null>('solve-job-source', () => null)
  const starting = useState<boolean>('solve-job-starting', () => false)
  const cancelling = useState<boolean>('solve-job-cancelling', () => false)

  function closeSource() {
    source.value?.close()
    source.value = null
  }

  function listen(id: string) {
    closeSource()
    const es = new EventSource(`/api/jobs/${id}/events`)
    es.addEventListener('update', (event) => {
      const update = JSON.parse((event as MessageEvent).data) as SolveUpdate
      job.value.progress = update
      if (update.config) job.value.config = update.config
      if (update.phase === 'SOLVE_PHASE_PROGRESS') return
      job.value.latest = update
      if (update.score) {
        job.value.history.push({
          wallTimeSeconds: update.wallTimeSeconds ?? 0,
          overallQuality: update.score.overallQuality,
          objective: update.objective,
        })
      }
    })
    es.addEventListener('done', (event) => {
      const data = JSON.parse((event as MessageEvent).data) as {
        state: JobState
      }
      job.value.state = data.state
      closeSource()
    })
    es.onerror = () => {
      // The stream ends when the job finishes; only flag real failures.
      if (job.value.state === 'running') {
        job.value.error = 'event stream interrupted'
        job.value.state = 'error'
      }
      closeSource()
    }
    source.value = es
  }

  async function start(options: {
    preset?: string
    seed?: number
    model?: SchoolModel
    params: SolveParams
    config?: SolverConfig
  }) {
    if (starting.value || job.value.state === 'running') return
    starting.value = true
    job.value.error = null
    try {
      const response = await api.startJob(options)
      job.value = {
        jobId: response.id,
        state: 'running',
        model: response.model,
        latest: null,
        progress: null,
        history: [],
        error: null,
        config: null,
      }
      listen(response.id)
    } catch (error) {
      job.value.error = error instanceof Error ? error.message : String(error)
    } finally {
      starting.value = false
    }
  }

  // Re-solve the (possibly edited) current model; current placements become
  // previous placements so the stability preference keeps the schedule calm.
  async function resolveCurrent(params: SolveParams, config?: SolverConfig) {
    const model = job.value.model
    if (!model) return
    const placements = new Map(
      (job.value.latest?.snapshot?.lessons ?? []).map((sl) => [
        sl.lessonId,
        sl.placement,
      ]),
    )
    for (const lesson of model.lessons) {
      const placement = placements.get(lesson.id)
      if (placement) {
        lesson.hasPrevious = true
        lesson.previousPlacement = placement
      }
    }
    await start({ model, params, config })
  }

  async function cancel() {
    if (!job.value.jobId || cancelling.value) return
    cancelling.value = true
    try {
      await api.cancelJob(job.value.jobId)
    } catch (error) {
      job.value.error = error instanceof Error ? error.message : String(error)
    } finally {
      cancelling.value = false
    }
  }

  // Loads an imported timetable as the current state: the schedule,
  // conflicts, and scores pages render it immediately, and Solve re-solves
  // the imported model (imported placements are previous placements).
  async function loadImported(imported: ImportResponse) {
    closeSource()
    let score
    try {
      score = await api.score(imported.model, imported.snapshot)
    } catch {
      score = undefined
    }
    job.value = {
      jobId: null,
      state: null,
      model: imported.model,
      progress: null,
      latest: {
        phase: 'SOLVE_PHASE_DONE',
        status: 'SOLVE_STATUS_UNSPECIFIED',
        snapshot: imported.snapshot,
        objective: '0',
        score,
        validation: imported.validation,
        wallTimeSeconds: 0,
        message: 'imported timetable',
        bestBound: '0',
        solutionsFound: 0,
      },
      history: [],
      error: null,
      config: null,
    }
  }

  return {
    job,
    starting,
    cancelling,
    start,
    resolveCurrent,
    cancel,
    loadImported,
  }
}
