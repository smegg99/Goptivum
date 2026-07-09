// app/composables/useWorkspace.ts

import type { SchoolModel } from '~/types/arrango'
import type { EntityKind, RemoveResult } from '~/utils/entityOps'
import type { SubjectRoomRule, SubjectRoomRules } from '~/utils/roomRules'
import * as ops from '~/utils/entityOps'
import { deepClone, emptyModel } from '~/utils/model'
import { applyAllRules, applySubjectRule, deriveSubjectRule } from '~/utils/roomRules'
import {
  deserializeWorkspace,
  loadFromStorage,
  saveToStorage,
  serializeWorkspace,
} from '~/utils/workspaceStore'

export function useWorkspace() {
  const api = useArrangoApi()
  const model = useState<SchoolModel>('workspace-model', () => emptyModel())
  const rules = useState<SubjectRoomRules>('workspace-rules', () => ({}))

  function persist() {
    saveToStorage({ model: model.value, rules: rules.value })
  }

  function reload() {
    const saved = loadFromStorage()
    if (saved) {
      model.value = saved.model
      rules.value = saved.rules
      return
    }

    model.value = emptyModel()
    rules.value = {}
  }

  function loadEmpty() {
    model.value = emptyModel()
    rules.value = {}
    persist()
  }

  function setModel(nextModel: SchoolModel) {
    model.value = nextModel
    rules.value = seedRules(nextModel)
    persist()
  }

  async function loadPreset(preset: string, seed = 42) {
    setModel(await api.getSchool(preset, seed))
  }

  function mutate(fn: (draft: SchoolModel) => void) {
    const draft = deepClone(model.value)
    fn(draft)
    model.value = draft
    persist()
  }

  function addYear(value: Parameters<typeof ops.addYear>[1]) {
    mutate((draft) => ops.addYear(draft, value))
  }

  function addDivision(value: Parameters<typeof ops.addDivision>[1]) {
    mutate((draft) => ops.addDivision(draft, value))
  }

  function addGroup(value: Parameters<typeof ops.addGroup>[1]) {
    mutate((draft) => ops.addGroup(draft, value))
  }

  function addTeacher(value: Parameters<typeof ops.addTeacher>[1]) {
    mutate((draft) => ops.addTeacher(draft, value))
  }

  function addSubject(value: Parameters<typeof ops.addSubject>[1]) {
    mutate((draft) => ops.addSubject(draft, value))
  }

  function addRoom(value: Parameters<typeof ops.addRoom>[1]) {
    mutate((draft) => ops.addRoom(draft, value))
  }

  function addLesson(value: Parameters<typeof ops.addLesson>[1]) {
    mutate((draft) => ops.addLesson(draft, value))
  }

  function updateEntity(kind: EntityKind, id: number, patch: Record<string, unknown>) {
    mutate((draft) => ops.updateEntity(draft, kind, id, patch))
  }

  function removeEntity(kind: EntityKind, id: number): RemoveResult {
    const draft = deepClone(model.value)
    const result = ops.removeEntity(draft, kind, id)
    if (result.ok) {
      model.value = draft
      persist()
    }
    return result
  }

  function setSubjectRule(subjectId: number, rule: SubjectRoomRule) {
    const nextRules = { ...rules.value, [subjectId]: rule }
    rules.value = nextRules
    mutate((draft) => applySubjectRule(draft, subjectId, rule))
  }

  function exportJson(): string {
    return serializeWorkspace({ model: model.value, rules: rules.value })
  }

  function importJson(text: string) {
    const workspace = deserializeWorkspace(text)
    applyAllRules(workspace.model, workspace.rules)
    model.value = workspace.model
    rules.value = workspace.rules
    persist()
  }

  return {
    model,
    rules,
    reload,
    persist,
    loadEmpty,
    loadPreset,
    setModel,
    addYear,
    addDivision,
    addGroup,
    addTeacher,
    addSubject,
    addRoom,
    addLesson,
    updateEntity,
    removeEntity,
    setSubjectRule,
    exportJson,
    importJson,
  }
}

function seedRules(model: SchoolModel): SubjectRoomRules {
  return Object.fromEntries(
    model.subjects.map((subject) => [subject.id, deriveSubjectRule(model, subject.id)]),
  )
}
