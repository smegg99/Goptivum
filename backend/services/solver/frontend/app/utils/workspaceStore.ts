// app/utils/workspaceStore.ts

import type { SchoolModel } from '~/types/arrango'
import type { SubjectRoomRules } from '~/utils/roomRules'

export interface Workspace {
  model: SchoolModel
  rules: SubjectRoomRules
}

export const WORKSPACE_STORAGE_KEY = 'arrango-workspace'

interface WorkspaceEnvelope {
  version: 1
  model: SchoolModel
  rules?: SubjectRoomRules
}

export function serializeWorkspace(workspace: Workspace): string {
  return JSON.stringify({
    version: 1,
    model: workspace.model,
    rules: workspace.rules,
  } satisfies WorkspaceEnvelope)
}

export function deserializeWorkspace(text: string): Workspace {
  let envelope: WorkspaceEnvelope
  try {
    envelope = JSON.parse(text) as WorkspaceEnvelope
  } catch (error) {
    throw new Error(`workspace JSON is invalid: ${(error as Error).message}`)
  }

  if (!envelope || typeof envelope !== 'object' || !envelope.model) {
    throw new Error('workspace JSON is missing a model')
  }
  if (!Array.isArray(envelope.model.subjects)) {
    throw new Error('workspace JSON model is missing subjects')
  }

  const subjectIds = new Set(envelope.model.subjects.map((subject) => subject.id))
  const rules: SubjectRoomRules = {}
  for (const [key, rule] of Object.entries(envelope.rules ?? {})) {
    const subjectId = Number(key)
    if (subjectIds.has(subjectId)) rules[subjectId] = cleanRule(rule)
  }

  return { model: envelope.model, rules }
}

export function loadFromStorage(): Workspace | null {
  if (!import.meta.client) return null

  const text = window.localStorage.getItem(WORKSPACE_STORAGE_KEY)
  if (!text) return null

  try {
    return deserializeWorkspace(text)
  } catch {
    return null
  }
}

export function saveToStorage(workspace: Workspace): void {
  if (!import.meta.client) return

  window.localStorage.setItem(WORKSPACE_STORAGE_KEY, serializeWorkspace(workspace))
}

function cleanRule(value: unknown) {
  const rule = value as { allowed?: unknown, disallowed?: unknown }
  return {
    allowed: Array.isArray(rule?.allowed) ? rule.allowed.filter(isString) : [],
    disallowed: Array.isArray(rule?.disallowed) ? rule.disallowed.filter(isString) : [],
  }
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}
