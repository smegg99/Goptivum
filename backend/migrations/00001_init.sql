-- +goose Up
-- Nothing has launched, so this file is edited in place. A second migration
-- file becomes legitimate the day a real school runs a real database.

-- A plan is the unit of naming, copying, deletion and version history. The
-- document itself lives in object storage; this table is the index.
create table plans (
    id                bigint    not null primary key,
    owner_identity_id text      not null,
    name              text      not null,
    school_year       text      not null default '',
    created_at        timestamp not null,
    updated_at        timestamp not null
);

-- Every listing is one owner's plans: owner_identity_id is a repository
-- filter, not a permission graph.
create index plans_owner_idx on plans (owner_identity_id);

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
    primary key (plan_id, version)
);

-- +goose Down
drop table plan_versions;
drop table plans;
