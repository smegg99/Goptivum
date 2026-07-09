// app/types/arrango.ts

// Shapes match protojson output of proto/arrango/v1. Note: int64 fields
// (penalties, objective, weights, seeds) arrive as strings.

export interface Placement {
  dayId: number
  startPeriod: number
  roomId: number
}

export interface Day {
  id: number
  name: string
  periodCount: number
}

export interface Period {
  id: number
  name: string
}

export interface Year {
  id: number
  name: string
  level: number
  priority: number
}

// Where a stored count came from; '' when absent. UNKNOWN means the count
// field must be ignored.
export type CountSource =
  | 'COUNT_SOURCE_UNKNOWN'
  | 'COUNT_SOURCE_IMPORTED'
  | 'COUNT_SOURCE_MANUAL'
  | 'COUNT_SOURCE_DEFAULT'

export interface Division {
  id: number
  name: string
  yearId: number
  studentCount?: number
  countSource?: CountSource
  sourceRef?: string
}

// One physical way of cutting a division into groups. Two groups may run in
// parallel iff they are different groups of one split, or both belong to
// OPEN splits; FIXED membership (gender, religion choice) overlaps
// everything outside its own split.
export type SplitKind = 'SPLIT_KIND_OPEN' | 'SPLIT_KIND_FIXED'

export interface Split {
  id: number
  name: string
  divisionId: number
  kind?: SplitKind
}

export interface Group {
  id: number
  name: string
  divisionId: number
  studentCount?: number
  countSource?: CountSource
  sourceRef?: string
  // Owning split; absent = implicit private OPEN split.
  splitId?: number
}

export interface Teacher {
  id: number
  name: string
  sourceRef?: string
}

export interface Subject {
  id: number
  name: string
  prefersBlocks: boolean
  sourceRef?: string
}

export interface Room {
  id: number
  name: string
  // Canonical short room code ('106', 'pe3', 'SKat'); exact-match semantics.
  designator: string
  capacity?: number
  capacitySource?: CountSource
  sourceRef?: string
}

// One student-set attending a lesson. groupId 0/absent = whole division.
export interface Participant {
  divisionId: number
  groupId?: number
}

export interface LessonInstance {
  id: number
  participants: Participant[]
  subjectId: number
  teacherId: number
  duration: number
  // Empty = unrestricted. Disallowed always wins.
  allowedRoomDesignators?: string[]
  disallowedRoomDesignators?: string[]
  observedRoomDesignators?: string[]
  fixedRoomId?: number
  edge?: EdgePlacement
  parallelBlockId: number
  requiresTeacher: boolean
  requiresRoom: boolean
  locked: boolean
  lockedPlacement?: Placement
  hasPrevious: boolean
  previousPlacement?: Placement
}

export interface DailyLoadRule {
  divisionId?: number
  groupId?: number
  minPerDay?: number
  maxPerDay?: number
  targetPerDay?: number
  allowedDeviation?: number
  deviationWeight?: string
  imbalanceWeight?: string
  overloadWeight?: string
  underloadWeight?: string
}

export type BlockTarget =
  | 'TARGET_UNSPECIFIED'
  | 'TARGET_DIVISION'
  | 'TARGET_GROUP'
  | 'TARGET_TEACHER'
  | 'TARGET_ROOM'

export interface ExternalBlock {
  id: number
  name: string
  target: BlockTarget
  targetId: number
  dayId: number
  startPeriod: number
  duration: number
}

export interface Weights {
  studentGapBase: string
  teacherGapBase: string
  lateStudentLessonBase: string
  lateTeacherFinishBase: string
  subjectSplitBase: string
  blockBreakBase: string
  roomChangeBase: string
  stabilityMoveBase: string
  expectedBadPerLesson: string
  lateThresholdPeriod: number
  gapCapPerDay: number
  singleLessonDayBase?: string
}

// Rule modes: every comfort rule has a three-position switch; DEFAULT in an
// override keeps whatever the earlier layer decided.
export type RuleMode =
  | 'RULE_MODE_DEFAULT'
  | 'RULE_MODE_HARD'
  | 'RULE_MODE_SOFT'
  | 'RULE_MODE_OFF'

// Empty scope fields = school-wide; within one list the LAST matching
// override wins (no implicit specificity ranking).
export interface RuleOverride {
  rule: string
  mode?: RuleMode
  weight?: string
  param?: number
  yearId?: number
  divisionId?: number
  subjectId?: number
  teacherId?: number
}

// Profile ('', 'default', 'dobry_plan', 'relaxed') applied first, then the
// overrides in order.
export interface RuleConfig {
  profile?: string
  overrides?: RuleOverride[]
}

export interface Preference {
  kind: 'KIND_UNSPECIFIED' | 'KIND_PREFER_EARLY' | 'KIND_MAX_LESSONS_PER_DAY'
  yearId: number
  divisionId: number
  subjectId: number
  weight: string
  param: number
}

export interface SchoolModel {
  name: string
  days: Day[]
  periods: Period[]
  years: Year[]
  divisions: Division[]
  groups: Group[]
  teachers: Teacher[]
  subjects: Subject[]
  rooms: Room[]
  lessons: LessonInstance[]
  externalBlocks: ExternalBlock[]
  weights: Weights
  preferences: Preference[]
  dailyLoadRules?: DailyLoadRule[]
  splits?: Split[]
  ruleConfig?: RuleConfig
  lessonLinks?: LessonLink[]
}

// Relative-placement constraint between 2+ lessons (always hard).
export type LessonLinkKind =
  | 'LESSON_LINK_UNSPECIFIED'
  | 'LESSON_LINK_SAME_DAY'
  | 'LESSON_LINK_DIFFERENT_DAY'
  | 'LESSON_LINK_CONSECUTIVE'

export interface LessonLink {
  id: number
  kind: LessonLinkKind
  lessonIds: number[]
  ordered?: boolean
}

// Edge-of-day placement, relative to the lesson's student streams.
export type EdgePlacement =
  | 'EDGE_NONE'
  | 'EDGE_FIRST'
  | 'EDGE_LAST'
  | 'EDGE_EITHER'

export interface ScheduledLesson {
  lessonId: number
  placement: Placement
}

export interface ScheduleSnapshot {
  lessons: ScheduledLesson[]
}

// Structured, language-agnostic issue localization (shared by hard conflicts
// and soft issues).
export type EntityKind =
  | 'ENTITY_KIND_UNSPECIFIED'
  | 'ENTITY_KIND_DIVISION'
  | 'ENTITY_KIND_GROUP'
  | 'ENTITY_KIND_TEACHER'
  | 'ENTITY_KIND_ROOM'
  | 'ENTITY_KIND_SUBJECT'
  | 'ENTITY_KIND_YEAR'
  | 'ENTITY_KIND_EXTERNAL_BLOCK'

export interface EntityRef {
  kind: EntityKind
  id: number
}

export interface LessonRef {
  lessonId: number
  dayId?: number
  startPeriod?: number
  duration?: number
  roomId?: number
}

export interface TimeSpan {
  dayId: number
  startPeriod: number
  periodSpan: number
}

export interface Conflict {
  kind: string
  message: string
  lessonIds: number[]
  entityId: number
  dayId: number
  period: number
  entities?: EntityRef[]
  lessons?: LessonRef[]
  spans?: TimeSpan[]
}

export interface ValidationReport {
  valid: boolean
  conflicts: Conflict[]
}

export interface PenaltyItem {
  category: string
  penalty: string
  count: number
}

export interface EntityScore {
  entityId: number
  name: string
  quality: number
  penalty: string
  items: PenaltyItem[]
}

export interface SoftIssue {
  category: string
  entity: string
  entityId: number
  teacher: boolean
  dayId: number
  period: number
  count: number
  penalty: string
  // Violates a rule the school set to HARD: an ERROR, never a warning.
  configHard?: boolean
  entities?: EntityRef[]
  lessons?: LessonRef[]
  spans?: TimeSpan[]
}

// One natural-unit rating metric: subscore = 100 * 2^(-rate/halfLife),
// absolute scale — 100 means pristine (zero violations of that metric).
export interface MetricScore {
  key: string
  teachers?: boolean
  applicable?: boolean
  rate: number
  subscore: number
  count: string
}

export interface ScoreReport {
  // Absolute metric-based qualities (100 == pristine); totalPenalty stays
  // the raw search objective for solver debugging.
  overallQuality: number
  allStudentsQuality: number
  allTeachersQuality: number
  totalPenalty: string
  divisionScores: EntityScore[]
  yearScores: EntityScore[]
  teacherScores: EntityScore[]
  globalItems: PenaltyItem[]
  softIssues: SoftIssue[]
  metricScores?: MetricScore[]
  // Hygiene findings (INFO): reported, never scored, never blocking.
  infoIssues?: SoftIssue[]
}

export type VerdictTier =
  | 'TIER_UNSPECIFIED'
  | 'TIER_PRISTINE'
  | 'TIER_WARNINGS'
  | 'TIER_ERRORS'

export interface ScheduleVerdict {
  tier: VerdictTier
  errors?: number
  warnings?: number
  infos?: number
  // Of `errors`, how many are config-hard rule violations (vs structural).
  configHardErrors?: number
}

export type SolveStage =
  | 'SOLVE_STAGE_UNSPECIFIED'
  | 'SOLVE_STAGE_PREFLIGHT'
  | 'SOLVE_STAGE_CANDIDATES'
  | 'SOLVE_STAGE_CONSTRUCT'
  | 'SOLVE_STAGE_DIRECT'
  | 'SOLVE_STAGE_LNS'
  | 'SOLVE_STAGE_VALIDATE'
  | 'SOLVE_STAGE_DONE'
  | 'SOLVE_STAGE_EXPLAIN'

// What the solver is doing right now; carried on every PROGRESS update.
export interface SolveProgress {
  stage: SolveStage
  detail?: string
  candidates?: string
  lnsPass?: number
  lnsNeighborhood?: number
  lnsNeighborhoodsTotal?: number
  lnsAccepted?: number
  lnsRejectedWorse?: number
  lnsRejectedInvalid?: number
  stageElapsedSeconds?: number
}

export type SolvePhase =
  | 'SOLVE_PHASE_UNSPECIFIED'
  | 'SOLVE_PHASE_STARTED'
  | 'SOLVE_PHASE_SOLUTION'
  | 'SOLVE_PHASE_DONE'
  | 'SOLVE_PHASE_PROGRESS'

export type SolveStatus =
  | 'SOLVE_STATUS_UNSPECIFIED'
  | 'SOLVE_STATUS_OPTIMAL'
  | 'SOLVE_STATUS_FEASIBLE'
  | 'SOLVE_STATUS_INFEASIBLE'
  | 'SOLVE_STATUS_CANCELLED'
  | 'SOLVE_STATUS_TIMEOUT'
  | 'SOLVE_STATUS_ERROR'

export type SolveMode =
  | 'SOLVE_MODE_UNSPECIFIED'
  | 'SOLVE_MODE_VIABILITY_CHECK'
  | 'SOLVE_MODE_CONSTRUCT_FAST'
  | 'SOLVE_MODE_CONSTRUCT_CLEAN'
  | 'SOLVE_MODE_ASSIGN_ROOMS'
  | 'SOLVE_MODE_REPAIR'
  | 'SOLVE_MODE_POLISH'
  | 'SOLVE_MODE_FULL_PIPELINE'

// Runtime solver configuration. Sent optionally with a solve request and
// echoed by the solver on every update (the config snapshot a result is
// compared by). All fields optional; the server fills defaults.
export interface SolverConfig {
  mode?: SolveMode
  totalTimeSeconds?: number
  numWorkers?: number
  randomSeed?: number
  exactRoomThreshold?: number
  maxCandidatesPerLesson?: number
  disableWarmStart?: boolean
  portfolioSeeds?: number
  disableLns?: boolean
  lnsIterations?: number
  lnsSecondsPerNeighborhood?: number
  poolStrategy?: string
  poolSafety?: string
  unknownCapacityPolicy?: string
  unplacedPolicy?: string
  ignorePreviousPlacements?: boolean
  ignoreViabilityBlock?: boolean
  weights?: Weights
  unplacedPenalty?: string
  maxStreamsPerDivision?: number
  // Echo of the effective rule layers (model's, then the request's).
  ruleConfig?: RuleConfig
  disableInfeasibilityExplainer?: boolean
  explainBudgetSeconds?: number
}

// One member of an infeasibility explanation — a model input the user can
// act on (never a solver internal).
export interface InfeasibleCoreItem {
  kind: string
  rule?: string
  entityId?: number
  entityName?: string
  lessonIds?: number[]
  message: string
}

// "These inputs cannot be satisfied together."
export interface InfeasibleCore {
  items?: InfeasibleCoreItem[]
  minimal?: boolean
  message: string
  hints?: InfeasibleCoreItem[]
}

export interface SolveUpdate {
  phase: SolvePhase
  status: SolveStatus
  snapshot?: ScheduleSnapshot
  objective: string
  score?: ScoreReport
  validation?: ValidationReport
  wallTimeSeconds: number
  message: string
  bestBound: string
  solutionsFound: number
  config?: SolverConfig
  verdict?: ScheduleVerdict
  progress?: SolveProgress
  infeasibleCore?: InfeasibleCore
}

export interface SolveParams {
  maxTimeSeconds: number
  numWorkers: number
  randomSeed: number
  fromScratch?: boolean
}

export type JobState = 'running' | 'done' | 'cancelled' | 'error'
