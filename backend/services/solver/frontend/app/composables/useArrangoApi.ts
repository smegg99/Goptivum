// app/composables/useArrangoApi.ts

import type {
  SchoolModel,
  ScheduleSnapshot,
  ScoreReport,
  SolveParams,
  SolverConfig,
  ValidationReport,
} from '~/types/arrango'

export interface ImportSummary {
  divisions: number
  groups: number
  teachers: number
  rooms: number
  subjects: number
  lessons: number
  days: number
  periods: number
}

export interface ImportResponse {
  model: SchoolModel
  snapshot: ScheduleSnapshot
  validation: ValidationReport
  warnings: string[]
  summary: ImportSummary
}

export interface StartJobResponse {
  id: string
  model: SchoolModel
}

export function useArrangoApi() {
  async function getSchool(preset: string, seed: number): Promise<SchoolModel> {
    return await $fetch<SchoolModel>('/api/school', {
      query: { preset, seed },
    })
  }

  async function validate(
    model: SchoolModel,
    snapshot: ScheduleSnapshot,
  ): Promise<ValidationReport> {
    return await $fetch<ValidationReport>('/api/validate', {
      method: 'POST',
      body: { model, snapshot },
    })
  }

  async function score(
    model: SchoolModel,
    snapshot: ScheduleSnapshot,
  ): Promise<ScoreReport> {
    return await $fetch<ScoreReport>('/api/score', {
      method: 'POST',
      body: { model, snapshot },
    })
  }

  async function startJob(body: {
    preset?: string
    seed?: number
    model?: SchoolModel
    params: SolveParams
    config?: SolverConfig
  }): Promise<StartJobResponse> {
    return await $fetch<StartJobResponse>('/api/jobs', {
      method: 'POST',
      body,
    })
  }

  async function cancelJob(id: string): Promise<void> {
    await $fetch(`/api/jobs/${id}/cancel`, { method: 'POST' })
  }

  async function exportArchive(
    model: SchoolModel,
    snapshot: ScheduleSnapshot,
  ): Promise<Blob> {
    return await $fetch<Blob>('/api/export', {
      method: 'POST',
      body: { model, snapshot },
      responseType: 'blob',
    })
  }

  async function importArchive(file: File): Promise<ImportResponse> {
    const body = new FormData()
    body.append('archive', file)
    return await $fetch<ImportResponse>('/api/import', {
      method: 'POST',
      body,
    })
  }

  return {
    getSchool,
    validate,
    score,
    startJob,
    cancelJob,
    importArchive,
    exportArchive,
  }
}
