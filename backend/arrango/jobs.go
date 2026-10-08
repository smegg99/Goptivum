// arrango/jobs.go

package arrango

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/smegg99/s99ab/data/db"

	"github.com/smegg99/goptivum/backend/common/messages"
	"github.com/smegg99/goptivum/solveclient"
)

// ErrNoJob means no job has that id.
var ErrNoJob = errors.New("arrango: no such job")

// A job's status. Pending and running are the two that count against a node's
// concurrency; the rest are terminal.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Starter is who started a solve: the identity and its display name then.
type Starter struct {
	ID   string
	Name string
}

// Job is one dispatched solve.
type Job struct {
	ID          int64
	PlanID      int64
	PlanVersion int64
	// SourceResultID is the result the solve reads instead of the version, nil
	// when it reads the version; the version is then that result's source.
	SourceResultID *int64
	NodeID         int64
	// NodeLabel is the node's label now, read with the job.
	NodeLabel        string
	Status           string
	NumWorkers       uint32
	TimeLimitSeconds float64
	Improve          bool
	StallStopSeconds float64
	StartedBy        Starter
	// Profile is the rule profile the solve used in place of the plan's rules, nil for the plan's own.
	Profile *solveclient.Profile
	// ResultID is the result the solve kept, and nil when there is nothing to
	// open. There is no separate "did it solve" flag: a solve that produced a
	// schedule nobody could store leaves nothing a user can see, so two fields
	// would only ever disagree.
	ResultID *int64
	// Reason is why the solve failed or kept nothing, as a message key, empty
	// when there is nothing to explain.
	Reason string
	// Quality is the node's own 0-100 rating, nil when nothing was produced.
	Quality *float64
	// Verdict is the one-glance tier over errors and warnings.
	Verdict string
	// Explanation names the inputs that cannot all hold, after a solve the
	// node proved infeasible. Empty when the question did not arise.
	Explanation string
	Detail      string
	// PeakRSSBytes and CPUSeconds are what the solve cost, nil when the node
	// did not say.
	PeakRSSBytes *int64
	CPUSeconds   *float64
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

// NewJob is a solve about to be dispatched.
type NewJob struct {
	PlanID  int64
	Version int64
	// SourceResultID is the result the solve reads, 0 when it reads Version.
	SourceResultID   int64
	NodeID           int64
	Workers          uint32
	TimeLimitSeconds float64
	Improve          bool
	StallStopSeconds float64
	StartedBy        Starter
	Profile          *solveclient.Profile
}

// Jobs is the solve_jobs table.
type Jobs struct {
	conn   *sql.DB
	driver string
}

// NewJobs binds the job table to one pool.
func NewJobs(conn *sql.DB, driver string) *Jobs {
	return &Jobs{conn: conn, driver: driver}
}

func (j *Jobs) rebind(query string) string { return db.Rebind(j.driver, query) }

// Create opens a job in pending. It is created before anything is dialled, so
// a dispatch that dies still leaves a row saying it was attempted.
func (j *Jobs) Create(ctx context.Context, open NewJob) (Job, error) {
	id, err := newID()
	if err != nil {
		return Job{}, err
	}
	job := Job{
		ID: id, PlanID: open.PlanID, PlanVersion: open.Version, NodeID: open.NodeID,
		Status: StatusPending, NumWorkers: open.Workers, StartedBy: open.StartedBy,
		Profile: open.Profile, CreatedAt: now(),
		TimeLimitSeconds: open.TimeLimitSeconds, Improve: open.Improve, StallStopSeconds: open.StallStopSeconds,
	}
	var source any
	if open.SourceResultID != 0 {
		read := open.SourceResultID
		job.SourceResultID, source = &read, read
	}
	profileName, profileID, rules, err := profileColumns(job.Profile)
	if err != nil {
		return Job{}, err
	}
	query := j.rebind(`insert into solve_jobs
		(id, plan_id, plan_version, source_result_id, node_id, status, num_workers, started_by,
		 started_by_name, rule_profile_name, rule_profile_id, rule_config, detail, created_at, time_limit_seconds, improve, stall_stop_seconds)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if _, err := j.conn.ExecContext(ctx, query,
		job.ID, job.PlanID, job.PlanVersion, source, job.NodeID, job.Status, job.NumWorkers,
		job.StartedBy.ID, job.StartedBy.Name, profileName, profileID, rules, "", job.CreatedAt, job.TimeLimitSeconds, job.Improve, job.StallStopSeconds); err != nil {
		return Job{}, fmt.Errorf("arrango: create job: %w", err)
	}
	return job, nil
}

// profileColumns is how a job row stores its profile: empty name, null id and empty rules for none.
func profileColumns(profile *solveclient.Profile) (string, any, string, error) {
	if profile == nil {
		return "", nil, "", nil
	}
	rules, err := EncodeRules(profile.Rules)
	if err != nil {
		return "", nil, "", err
	}
	var id any
	if profile.ID != 0 {
		id = profile.ID
	}
	return profile.Name, id, rules, nil
}

// Start moves a job into running.
func (j *Jobs) Start(ctx context.Context, id int64) error {
	query := j.rebind(`update solve_jobs set status = ?, started_at = ? where id = ? and status = ?`)
	_, err := j.conn.ExecContext(ctx, query, StatusRunning, now(), id, StatusPending)
	if err != nil {
		return fmt.Errorf("arrango: start job: %w", err)
	}
	return nil
}

// Grant records the node's first echoed settings while a solve is running.
func (j *Jobs) Grant(ctx context.Context, id int64, seconds float64, workers uint32, stall float64) error {
	query := j.rebind(`update solve_jobs set time_limit_seconds = coalesce(nullif(?, 0), time_limit_seconds), num_workers = coalesce(nullif(?, 0), num_workers), stall_stop_seconds = ? where id = ?`)
	_, err := j.conn.ExecContext(ctx, query, seconds, workers, stall, id)
	if err != nil {
		return fmt.Errorf("arrango: record granted solve time: %w", err)
	}
	return nil
}

// Outcome is everything a finished solve is known by.
//
// A struct because a finish has outgrown a parameter list: what a school reads
// about a solve is its status, how good the result is, whether it is clean, and
// why it could not be done.
type Outcome struct {
	Status string
	// Reason is why the solve failed or kept nothing, as a message key.
	Reason string
	Detail string
	// ResultID is the result the timetable was kept as, nil when there is
	// nothing to open.
	ResultID *int64
	// Quality is the node's 0-100 rating, nil when nothing was produced.
	Quality *float64
	// Verdict is the node's tier over errors and warnings.
	Verdict string
	// Explanation names the inputs that cannot all hold.
	Explanation string
	// Workers is what the node ran the solve with; 0 keeps the job's own.
	Workers          uint32
	TimeLimitSeconds float64
	// PeakRSSBytes and CPUSeconds are what the solve cost; 0 is unknown.
	PeakRSSBytes uint64
	CPUSeconds   float64
}

// Finish closes a job.
//
// ResultID is set only when a solution frame arrived, its snapshot was kept,
// and that snapshot was stored as a result. A status field on a progress frame
// and a solutions_found counter both report a schedule that may not exist here:
// every callback in the solver sets FEASIBLE unconditionally, and a solution
// that fails validation is dropped before it reaches the stream.
func (j *Jobs) Finish(ctx context.Context, id int64, outcome Outcome) error {
	switch outcome.Status {
	case StatusDone, StatusFailed, StatusCancelled:
	default:
		return fmt.Errorf("arrango: %q is not a terminal status", outcome.Status)
	}
	query := j.rebind(`update solve_jobs
		set status = ?, reason = ?, detail = ?, result_id = ?, quality = ?,
		    verdict = ?, explanation = ?, num_workers = coalesce(?, num_workers), time_limit_seconds = coalesce(?, time_limit_seconds),
		    peak_rss_bytes = ?, cpu_seconds = ?, finished_at = ? where id = ?`)
	var resultID, quality, workers, seconds, peak, cpu any
	if outcome.ResultID != nil {
		resultID = *outcome.ResultID
	}
	if outcome.Quality != nil {
		quality = *outcome.Quality
	}
	if outcome.Workers > 0 {
		workers = outcome.Workers
	}
	if outcome.TimeLimitSeconds > 0 {
		seconds = outcome.TimeLimitSeconds
	}
	if outcome.PeakRSSBytes > 0 {
		peak = int64(outcome.PeakRSSBytes)
	}
	if outcome.CPUSeconds > 0 {
		cpu = outcome.CPUSeconds
	}
	result, err := j.conn.ExecContext(ctx, query, outcome.Status, outcome.Reason, outcome.Detail,
		resultID, quality, outcome.Verdict, outcome.Explanation, workers, seconds, peak, cpu, now(), id)
	if err != nil {
		return fmt.Errorf("arrango: finish job: %w", err)
	}
	// A finish that closed nothing leaves a job this server still counts
	// against its node, so it is an error rather than a quiet success.
	closed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("arrango: finish job: %w", err)
	}
	if closed == 0 {
		return fmt.Errorf("%w: %d", ErrNoJob, id)
	}
	return nil
}

// jobSelect reads jobs with their node's label; a node row is never deleted.
const jobSelect = `select j.id, j.plan_id, j.plan_version, j.source_result_id, j.node_id, n.label, j.status,
	j.num_workers, j.time_limit_seconds, j.improve, j.stall_stop_seconds, j.started_by, j.started_by_name, j.rule_profile_name, j.rule_profile_id, j.rule_config,
	j.result_id, j.reason, j.quality,
	j.verdict, j.explanation, j.detail, j.peak_rss_bytes, j.cpu_seconds,
	j.created_at, j.started_at, j.finished_at
	from solve_jobs j join arrango_nodes n on n.id = j.node_id`

func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var job Job
	var started, finished sql.NullTime
	var source, resultID, peak, profileID sql.NullInt64
	var quality, cpu sql.NullFloat64
	var profileName, rules string
	if err := row.Scan(&job.ID, &job.PlanID, &job.PlanVersion, &source, &job.NodeID, &job.NodeLabel,
		&job.Status, &job.NumWorkers, &job.TimeLimitSeconds, &job.Improve, &job.StallStopSeconds, &job.StartedBy.ID, &job.StartedBy.Name, &profileName, &profileID,
		&rules, &resultID, &job.Reason, &quality, &job.Verdict, &job.Explanation, &job.Detail, &peak, &cpu,
		&job.CreatedAt, &started, &finished); err != nil {
		return Job{}, err
	}
	if profileName != "" {
		decoded, err := DecodeRules(rules)
		if err != nil {
			return Job{}, err
		}
		job.Profile = &solveclient.Profile{Name: profileName, ID: profileID.Int64, Rules: decoded}
	}
	job.CreatedAt = job.CreatedAt.UTC()
	if source.Valid {
		job.SourceResultID = &source.Int64
	}
	if resultID.Valid {
		job.ResultID = &resultID.Int64
	}
	if quality.Valid {
		job.Quality = &quality.Float64
	}
	if peak.Valid {
		job.PeakRSSBytes = &peak.Int64
	}
	if cpu.Valid {
		job.CPUSeconds = &cpu.Float64
	}
	if started.Valid {
		stamp := started.Time.UTC()
		job.StartedAt = &stamp
	}
	if finished.Valid {
		stamp := finished.Time.UTC()
		job.FinishedAt = &stamp
	}
	return job, nil
}

// Get is one job by id.
func (j *Jobs) Get(ctx context.Context, id int64) (Job, error) {
	query := j.rebind(jobSelect + ` where j.id = ?`)
	job, err := scanJob(j.conn.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNoJob
	}
	if err != nil {
		return Job{}, fmt.Errorf("arrango: get job: %w", err)
	}
	return job, nil
}

// ForPlan is one plan's jobs, newest first.
func (j *Jobs) ForPlan(ctx context.Context, planID int64) ([]Job, error) {
	return j.list(ctx, j.rebind(jobSelect+` where j.plan_id = ? order by j.created_at desc, j.id`), planID)
}

// Filter narrows List: the statuses (none means every one), and the page.
type Filter struct {
	Statuses []string
	Offset   int
	Limit    int
}

// List is one page of the jobs of every plan out of Trash, whoever owns it,
// newest first, and how many match in all.
func (j *Jobs) List(ctx context.Context, filter Filter) ([]Job, int, error) {
	where, args := ` where j.plan_id in (select id from plans where deleted_at is null)`, []any{}
	if len(filter.Statuses) > 0 {
		where += ` and j.status in (?` + strings.Repeat(`, ?`, len(filter.Statuses)-1) + `)`
		for _, status := range filter.Statuses {
			args = append(args, status)
		}
	}
	var total int
	count := j.rebind(`select count(*) from solve_jobs j` + where)
	if err := j.conn.QueryRowContext(ctx, count, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("arrango: count jobs: %w", err)
	}
	page := j.rebind(jobSelect + where + ` order by j.created_at desc, j.id limit ? offset ?`)
	jobs, err := j.list(ctx, page, append(args, filter.Limit, filter.Offset)...)
	return jobs, total, err
}

// ForNode is one page of the jobs this server ran on a node, newest first,
// and how many there are in all.
func (j *Jobs) ForNode(ctx context.Context, nodeID int64, offset, limit int) ([]Job, int, error) {
	var total int
	count := j.rebind(`select count(*) from solve_jobs where node_id = ?`)
	if err := j.conn.QueryRowContext(ctx, count, nodeID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("arrango: count node jobs: %w", err)
	}
	page := j.rebind(jobSelect + ` where j.node_id = ? order by j.created_at desc, j.id limit ? offset ?`)
	jobs, err := j.list(ctx, page, nodeID, limit, offset)
	return jobs, total, err
}

func (j *Jobs) list(ctx context.Context, query string, args ...any) ([]Job, error) {
	rows, err := j.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("arrango: list jobs: %w", err)
	}
	defer rows.Close()

	jobs := []Job{}
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("arrango: scan job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("arrango: list jobs: %w", err)
	}
	return jobs, nil
}

// CountActive is how many solves this server believes a node is carrying.
func (j *Jobs) CountActive(ctx context.Context, nodeID int64) (int, error) {
	query := j.rebind(`select count(*) from solve_jobs where node_id = ? and status in (?, ?)`)
	var count int
	if err := j.conn.QueryRowContext(ctx, query, nodeID, StatusPending, StatusRunning).
		Scan(&count); err != nil {
		return 0, fmt.Errorf("arrango: count active jobs: %w", err)
	}
	return count, nil
}

// CloseOrphans fails every job left open by a previous process.
//
// Only a running process can own an open row, so at boot there are none. A
// crash, an out-of-memory kill or a power loss skips Finish, and the rows it
// left behind would count against their node's concurrency for ever: after
// its solves at once of them the node is refused until someone edits the
// database by hand. Call this before the runner exists.
func (j *Jobs) CloseOrphans(ctx context.Context) (int64, error) {
	query := j.rebind(`update solve_jobs
		set status = ?, reason = ?, finished_at = ? where status in (?, ?)`)
	result, err := j.conn.ExecContext(ctx, query, StatusFailed, messages.KeySolveServerRestarted,
		now(), StatusPending, StatusRunning)
	if err != nil {
		return 0, fmt.Errorf("arrango: close orphaned jobs: %w", err)
	}
	closed, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return closed, nil
}
