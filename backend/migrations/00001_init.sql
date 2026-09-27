-- +goose Up
-- Nothing has launched, so this file is edited in place. A second migration
-- file becomes legitimate the day a real school runs a real database.

-- A plan is one project: the unit of naming, copying, deletion and version
-- history. The document itself lives in object storage; this table is the
-- index. A plan is created together with its first version, so none exists
-- with nothing to open.
create table plans (
    id                bigint    not null primary key,
    -- The one account that changes the plan. Every signed-in account reads
    -- every live plan; an administrator can give it to another account.
    owner_identity_id text      not null,
    name              text      not null,
    school_year       text      not null default '',
    created_at        timestamp not null,
    updated_at        timestamp not null,
    -- Set when the plan is moved to Trash, null while it is live. A trashed
    -- plan answers as absent everywhere except Trash itself and restore; the
    -- purge removes it for good once projects.trash_days have passed.
    deleted_at        timestamp,
    -- The project's own cap on the older versions it keeps, checkpoints not
    -- counted. Null follows the server's projects.backups_keep.
    backups_keep      integer,
    -- The project's own cap on the unpinned results it keeps. Null follows
    -- the server's projects.results_keep.
    results_keep      integer,
    -- The plan and version a fork or a duplicate was copied from; both null
    -- on a plan that was not. No foreign key: the origin may go to Trash and
    -- be removed for good, and the copy still says where it came from.
    origin_plan_id    bigint,
    origin_version    bigint
);

-- Each account's Trash lists the plans it owns, and the checkpoints and
-- results of the live plans it owns.
create index plans_owner_idx on plans (owner_identity_id);

-- The purge reads only trashed plans, oldest first.
create index plans_deleted_idx on plans (deleted_at);

-- One row per version, one immutable blob per row, parent pointers forming the
-- timeline. An upload names the version it came from and a stale parent is
-- refused, so (plan_id, version) as the primary key is what decides a race.
create table plan_versions (
    plan_id     bigint    not null references plans (id) on delete cascade,
    version     bigint    not null,
    parent      bigint,
    object_key  text      not null,
    size_bytes  bigint    not null,
    created_at  timestamp not null,
    created_by  text      not null,
    -- A school's timetable changes mid-year: one plan holds "this schedule
    -- from September, this one from February" and publishing resolves the
    -- right one per day.
    valid_from  date,
    valid_to    date,
    -- What changed since the parent, as counts per edit kind in JSON
    -- ({"added":{"teacher":3}}), never prose, so every reader words it in its
    -- own language. Null when the uploader sent none.
    summary     text,
    -- Chosen by the client for one upload. A retry that carries a key already
    -- stored is answered with that version, so a lost answer never looks like
    -- a conflict. Null on versions the server made itself.
    upload_key  text,
    -- A named version is a checkpoint: the backups cap never removes it.
    -- Deleting the checkpoint sets checkpoint_deleted_at and keeps the name,
    -- so the version stays while the checkpoint is in Trash and a restore
    -- names it again.
    checkpoint_name       text,
    checkpoint_deleted_at timestamp,
    primary key (plan_id, version)
);

-- Unique per plan. Both engines treat nulls as distinct, so versions without a
-- key never collide.
create unique index plan_versions_upload_key_idx on plan_versions (plan_id, upload_key);

-- The sweep asks whether any version still names an object.
create index plan_versions_object_key_idx on plan_versions (object_key);

-- Trash and its purge read checkpoints by deletion time.
create index plan_versions_checkpoint_deleted_idx on plan_versions (checkpoint_deleted_at);

-- Objects that lost the row naming them, waiting for the sweep. Documents are
-- keyed by content, so one object can back versions and results of several
-- plans; the sweep deletes an object only once no version, result or export
-- row names it, and only after since is over an hour old, which covers an
-- upload or an export that stores its object before its row.
-- The sweep reads this table and never lists the bucket.
create table object_garbage (
    object_key text      not null primary key,
    since      timestamp not null
);

-- The change listing's clock, one row. value counts every stamp written; epoch
-- names this database, so a desktop holding a point from another one (the
-- database was made again) is told to read everything again. The server
-- writes the row with a random epoch at the first stamp or listing.
create table change_clock (
    id    integer not null primary key check (id = 1),
    epoch text    not null,
    value bigint  not null
);

-- What changed last in each plan, per part: project (the plan row), versions
-- or results. Each change sets its parts' seq to the clock's next value in
-- the change's own transaction, so a plan changed often is still one row per
-- part, and the listing reads what moved after a point. No foreign key: a
-- plan removed for good keeps its project row, listed as removed.
create table plan_changes (
    plan_id bigint not null,
    part    text   not null,
    seq     bigint not null,
    primary key (plan_id, part)
);

-- The listing reads the parts stamped after a point, in stamp order.
create index plan_changes_seq_idx on plan_changes (seq);

-- A node this deployment has paired with. The fingerprint is the identity;
-- the address is only where it was last reached, so moving a node needs no
-- re-pairing. Revocation is a timestamp, not a delete, so a job that names a
-- revoked node still reads.
create table arrango_nodes (
    id          bigint    not null primary key,
    label       text      not null,
    address     text      not null,
    fingerprint text      not null,
    paired_at   timestamp not null,
    revoked_at  timestamp,
    -- This server's own limits on the node: its solves there at once, and the
    -- workers each gets. Never above the node's own ceilings for this server,
    -- which pairing reads and a change is checked against.
    solves_at_once    integer not null,
    workers_per_solve integer not null
);

-- One deployment pairs with one node once. A second row for the same
-- fingerprint would mean two registry entries racing over one peer.
create unique index arrango_nodes_fingerprint_idx on arrango_nodes (fingerprint);

-- One row per dispatched solve. result_id is set only from a solution frame
-- this server actually received and stored: a status field and a counter on an
-- unrelated frame both claim a schedule exists when none does.
create table solve_jobs (
    id           bigint    not null primary key,
    plan_id      bigint    not null references plans (id) on delete cascade,
    -- The version whose school the solve reads. The backups cap never removes
    -- it while the job is pending or running.
    plan_version bigint    not null,
    -- The result the solve reads instead of the version, null when it reads
    -- the version. The results cap and the Trash purge never remove it while
    -- the job is pending or running. No foreign key, as for result_id.
    source_result_id bigint,
    node_id      bigint    not null references arrango_nodes (id),
    status       text      not null,
    -- This server's clamp, replaced by the node's own once its final frame
    -- reports what the solve actually ran with.
    num_workers  integer   not null,
    time_limit_seconds double precision not null default 0,
    improve boolean not null default false,
    stall_stop_seconds double precision not null default 0,
    -- Who started the solve: the identity, and its display name at the time.
    started_by      text   not null,
    started_by_name text   not null default '',
    -- The rule profile the solve used in place of the plan's own rules: a
    -- built-in's name, or a school profile's name and id at the time; empty
    -- name and null id when it used the plan's rules. No foreign key: a
    -- deleted profile still names itself here.
    rule_profile_name text not null default '',
    rule_profile_id   bigint,
    -- The rule_config the solve request carried, as Arrango's RuleConfig in
    -- protobuf JSON, so its result is checked by the rules it was solved by
    -- whatever happened to the profile since. Empty with no profile.
    rule_config       text not null default '',
    -- The result the solve kept. Null means it produced nothing to open,
    -- which is the only honest reading: a job that solved but could not be
    -- stored has nothing a user can see. No foreign key: the results cap and
    -- the Trash purge remove results, and the job still says what it kept.
    result_id    bigint,
    -- Why the solve failed or kept nothing, as a message key
    -- (solve.infeasible, ...); empty when there is nothing to explain.
    reason       text      not null default '',
    detail       text      not null default '',
    -- The node's own 0-100 rating of what it produced, where 100 is pristine.
    -- Null when nothing was produced. Deliberately not the search objective,
    -- which moves with the weights and cannot rank two runs of one school.
    quality      real,
    -- The one-glance tier over errors and warnings: pristine, warnings, errors.
    verdict      text      not null default '',
    -- Why the model could not be satisfied, as the node worked it out: the
    -- named inputs that cannot all hold. "No feasible solution" tells a school
    -- nothing it can act on; this names the teacher and the lessons.
    explanation  text      not null default '',
    -- What the solve cost, from the node's final frame: the solver process's
    -- peak resident memory and its CPU time. Null when no final frame came or
    -- the node did not report it.
    peak_rss_bytes bigint,
    cpu_seconds    real,
    created_at   timestamp not null,
    started_at   timestamp,
    finished_at  timestamp
);

-- Listings are one plan's jobs or several plans' jobs, newest first.
create index solve_jobs_plan_idx on solve_jobs (plan_id, created_at);

-- Admission counts what a node is already running, so it is a filter.
create index solve_jobs_node_status_idx on solve_jobs (node_id, status);

-- A timetable a solve kept: a whole document, the school data the solve read
-- (source_version's, from the version itself or from another result) plus the
-- timetable, so editing the main file never loses it. A solve on this server's
-- nodes keeps it with its job; one solved on a device's own node is uploaded
-- with the device's name and no job.
-- Everything but the name, the pin and the Trash date is written once.
create table solve_results (
    id              bigint    not null primary key,
    plan_id         bigint    not null references plans (id) on delete cascade,
    -- The job that found it; null for a result solved on a device.
    job_id          bigint    references solve_jobs (id) on delete cascade,
    -- The device that solved it and uploaded it; null for a server solve.
    device          text,
    -- The key the device chose for its upload, so a retry keeps one result;
    -- null for a server solve.
    upload_key      text,
    source_version  bigint    not null,
    object_key      text      not null,
    size_bytes      bigint    not null,
    -- When its solve ended: on this server, or on the device.
    created_at      timestamp not null,
    -- Who started the solve, or uploaded the result solved on a device.
    started_by      text      not null,
    started_by_name text      not null default '',
    -- The job's rule profile and the rule_config its solve was sent, copied
    -- like started_by: a result's check sends the same rules.
    rule_profile_name text    not null default '',
    rule_profile_id   bigint,
    rule_config       text    not null default '',
    -- The node's 0-100 rating and its tier over errors and warnings.
    quality         real,
    verdict         text      not null default '',
    name            text,
    -- The results cap never removes a pinned result.
    pinned          boolean   not null default false,
    -- Set when the result is moved to Trash; the cap neither counts nor
    -- removes it there, and the purge removes it after projects.trash_days.
    deleted_at      timestamp
);

-- The results folder lists one plan's results, newest first, and the cap
-- counts them in the same order.
create index solve_results_plan_idx on solve_results (plan_id, created_at);

-- The sweep asks whether any result still names an object.
create index solve_results_object_key_idx on solve_results (object_key);

-- Trash and its purge read results by deletion time.
create index solve_results_deleted_idx on solve_results (deleted_at);

-- A device's retry finds the result its upload key already kept; two keys
-- with no upload behind them (null) never clash.
create unique index solve_results_upload_key_idx on solve_results (plan_id, upload_key);

-- A file built for export from a version or a result, kept in object storage
-- at object_key so a second request for it is not built again. Exactly one of
-- version and result_id is set. Both sources cascade, and every path that
-- removes a version, a result or a plan records these keys in object_garbage
-- first, so the files go with what they were built from. variant is what the
-- bytes depend on besides the source: the recipient a .gpf was encrypted to,
-- empty when plain; a file built with another variant is built again.
create table plan_exports (
    object_key text      not null primary key,
    plan_id    bigint    not null references plans (id) on delete cascade,
    version    bigint,
    result_id  bigint    references solve_results (id) on delete cascade,
    format     text      not null,
    variant    text      not null default '',
    size_bytes bigint    not null,
    created_at timestamp not null,
    foreign key (plan_id, version) references plan_versions (plan_id, version) on delete cascade,
    check ((version is null) <> (result_id is null))
);

-- The removal paths read one plan's, one version's or one result's files.
create index plan_exports_plan_idx on plan_exports (plan_id, version);
create index plan_exports_result_idx on plan_exports (result_id);

-- A rule profile of the school's own: a set of rule overrides a solve can
-- use in place of a plan's own rules. Every account sees every profile; only
-- the account that created one and administrators change or delete it. The
-- built-in profiles are the solver's and are not stored here.
create table rule_profiles (
    id              bigint    not null primary key,
    name            text      not null,
    -- The overrides as Arrango's RuleConfig in protobuf JSON: no profile name
    -- and no scope, because a profile serves every plan and scope ids belong
    -- to one.
    rules           text      not null,
    created_by      text      not null,
    created_by_name text      not null default '',
    created_at      timestamp not null,
    updated_at      timestamp not null
);

-- A profile is picked by name, so two with one name would be one too many.
create unique index rule_profiles_name_idx on rule_profiles (name);

-- The school's own key for .gpf Identity mode. One row, because the decision
-- was one key for the whole school rather than one per person: anyone signed
-- into the school opens any of its files.
--
-- The private half is stored sealed, never in the clear, so a database dump on
-- its own opens nothing. The public half is a recipient string and is not a
-- secret: it is what a file is encrypted TO.
create table school_key (
    id         integer   not null primary key check (id = 1),
    recipient  text      not null,
    sealed     blob      not null,
    created_at timestamp not null
);

-- Who changed which project, account, node or setting, and when, written as
-- each change succeeds. Names are copied as they were then, so an entry still
-- reads after what it names was renamed or removed; for the same reason there
-- are no foreign keys.
create table audit_log (
    id          bigint    not null primary key,
    at          timestamp not null,
    -- The account that did it: its identity id, and its display name then.
    actor_id    text      not null,
    actor_name  text      not null default '',
    -- What was done, as a key the desktop words (project.renamed, ...).
    action      text      not null,
    -- What it was done to: project, user, node, setting or storage, its id,
    -- and its name then.
    target_kind text      not null,
    target_id   text      not null,
    target_name text      not null default '',
    -- The action's values by name, as a JSON object of strings.
    details     text      not null default '{}'
);

-- The trail is read newest first, whole or for one kind.
create index audit_log_at_idx on audit_log (at);
create index audit_log_kind_idx on audit_log (target_kind, at);

-- How one account arranges its Library: folders of projects, their order,
-- pins and Recent, as one JSON document the server checks before it stores
-- it. identity_id is the Kratos identity; no other account reads the row.
-- version counts the saves, and a save names the version it read, so two
-- windows of one account never overwrite each other. No foreign key to
-- plans: a project removed for good is left out when the document is read.
create table account_libraries (
    identity_id text   not null primary key,
    version     bigint not null,
    document    text   not null
);

-- +goose Down
drop table account_libraries;
drop table plan_changes;
drop table change_clock;
drop table audit_log;
drop table school_key;
drop table rule_profiles;
drop table object_garbage;
drop table plan_exports;
drop table solve_results;
drop table solve_jobs;
drop table arrango_nodes;
drop table plan_versions;
drop table plans;
