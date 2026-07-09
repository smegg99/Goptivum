// app/composables/useSolverConfig.ts
//
// Shared, editable runtime SolverConfig sent with every solve. Defaults mirror
// the solver's built-ins; presets are convenience starting points. Only the
// full pipeline runs today — the pooling knobs are carried for the future
// staged pipeline and marked as such in the UI.

import type { SolverConfig, Weights } from '~/types/arrango'

export const SOLVE_MODES = [
  { value: 'SOLVE_MODE_FULL_PIPELINE', title: 'Full pipeline' },
  { value: 'SOLVE_MODE_VIABILITY_CHECK', title: 'Viability check (soon)' },
  { value: 'SOLVE_MODE_CONSTRUCT_FAST', title: 'Construct — fast (soon)' },
  { value: 'SOLVE_MODE_CONSTRUCT_CLEAN', title: 'Construct — clean (soon)' },
  { value: 'SOLVE_MODE_ASSIGN_ROOMS', title: 'Assign rooms (soon)' },
  { value: 'SOLVE_MODE_REPAIR', title: 'Repair (soon)' },
  { value: 'SOLVE_MODE_POLISH', title: 'Polish (soon)' },
]

export const CAPACITY_POLICIES = [
  { value: 'UNKNOWN_CAPACITY_ALLOW', title: 'Allow unknown-capacity rooms' },
  { value: 'UNKNOWN_CAPACITY_FORBID', title: 'Forbid unknown-capacity rooms' },
  { value: 'UNKNOWN_CAPACITY_ALLOW_PENALIZED', title: 'Allow but penalize' },
]

export const UNPLACED_POLICIES = [
  { value: 'UNPLACED_FORBID', title: 'Forbid (fail if not all placed)' },
  { value: 'UNPLACED_ALLOW_PENALIZED', title: 'Allow partial (penalize)' },
]

export const POOL_STRATEGIES = [
  { value: 'POOL_STRATEGY_AUTO', title: 'Auto' },
  { value: 'POOL_STRATEGY_EXACT_ONLY', title: 'Exact rooms only' },
  { value: 'POOL_STRATEGY_POOL_LARGE', title: 'Pool large room sets' },
]

function defaultWeights(): Weights {
  return {
    studentGapBase: '1000',
    teacherGapBase: '45',
    lateStudentLessonBase: '40',
    lateTeacherFinishBase: '20',
    subjectSplitBase: '70',
    blockBreakBase: '90',
    roomChangeBase: '10',
    stabilityMoveBase: '15',
    expectedBadPerLesson: '60',
    lateThresholdPeriod: 7,
    gapCapPerDay: 3,
  }
}

export function defaultConfig(): SolverConfig {
  return {
    mode: 'SOLVE_MODE_FULL_PIPELINE',
    totalTimeSeconds: 180,
    numWorkers: 0,
    randomSeed: 7,
    exactRoomThreshold: 3,
    maxCandidatesPerLesson: 0,
    disableWarmStart: false,
    portfolioSeeds: 0,
    disableLns: false,
    lnsIterations: 30,
    lnsSecondsPerNeighborhood: 2,
    poolStrategy: 'POOL_STRATEGY_AUTO',
    poolSafety: 'POOL_SAFETY_STRICT',
    unknownCapacityPolicy: 'UNKNOWN_CAPACITY_ALLOW',
    unplacedPolicy: 'UNPLACED_FORBID',
    ignorePreviousPlacements: false,
    ignoreViabilityBlock: false,
    weights: defaultWeights(),
  }
}

// Named presets (spec §config presets), tuned starting points.
export const CONFIG_PRESETS: Record<string, () => SolverConfig> = {
  default: () => defaultConfig(),
  'fast construction': () => ({
    ...defaultConfig(),
    totalTimeSeconds: 60,
    disableLns: true,
  }),
  'strict feasible': () => ({
    ...defaultConfig(),
    totalTimeSeconds: 300,
    unplacedPolicy: 'UNPLACED_FORBID',
    unknownCapacityPolicy: 'UNKNOWN_CAPACITY_FORBID',
  }),
  'polish existing': () => ({
    ...defaultConfig(),
    totalTimeSeconds: 300,
    ignorePreviousPlacements: false,
  }),
  experimental: () => ({
    ...defaultConfig(),
    portfolioSeeds: 4,
    lnsIterations: 60,
  }),
}

export function useSolverConfig() {
  const config = useState<SolverConfig>('solver-config', () => defaultConfig())
  function applyPreset(name: string) {
    const make = CONFIG_PRESETS[name]
    if (make) config.value = make()
  }
  function reset() {
    config.value = defaultConfig()
  }
  return { config, applyPreset, reset }
}
