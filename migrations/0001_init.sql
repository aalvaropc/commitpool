create extension if not exists pgcrypto;
create table workshops (
  id uuid primary key default gen_random_uuid(),
  owner_token_hash bytea not null unique,
  share_token_hash bytea not null unique,
  title text not null, description text not null default '',
  duration_min int not null check (duration_min > 0),
  timezone text not null,
  minimum int not null check (minimum >= 1), capacity int not null check (capacity >= minimum),
  price_minor bigint not null check (price_minor > 0), currency char(3) not null,
  payment_window_min int not null default 1440 check (payment_window_min >= 1),
  status text not null default 'COLLECTING',
  resolution text not null default 'OPEN',
  created_at timestamptz not null default now(), updated_at timestamptz not null default now());
create table candidate_dates (
  id uuid primary key default gen_random_uuid(), workshop_id uuid not null references workshops(id),
  starts_at timestamptz not null, ends_at timestamptz not null, check (ends_at > starts_at));
create table proposals (
  id uuid primary key default gen_random_uuid(), workshop_id uuid not null references workshops(id),
  version int not null, chosen_date_id uuid not null references candidate_dates(id),
  price_minor bigint not null, currency char(3) not null, minimum int not null, capacity int not null,
  opens_at timestamptz not null, closes_at timestamptz not null, starts_at timestamptz not null,
  created_at timestamptz not null default now(), unique (workshop_id, version));
create table participants (
  id uuid primary key default gen_random_uuid(), workshop_id uuid not null references workshops(id),
  display_name text not null, access_token_hash bytea not null unique, created_at timestamptz not null default now());
create table availabilities (
  id uuid primary key default gen_random_uuid(), participant_id uuid not null references participants(id),
  raw_message text not null, interpretation jsonb not null, corrected jsonb,
  needs_clarification boolean not null default false, model_meta jsonb not null default '{}',
  created_at timestamptz not null default now());
create table enrollments (
  id uuid primary key default gen_random_uuid(), workshop_id uuid not null references workshops(id),
  participant_id uuid not null references participants(id), proposal_id uuid not null references proposals(id),
  status text not null, consent_at timestamptz not null, consent_version text not null,
  seat_reserved_at timestamptz not null, seat_expires_at timestamptz not null,
  approval_cancelled_at timestamptz, released_at timestamptz,
  created_at timestamptz not null default now(), updated_at timestamptz not null default now());
create unique index one_active_enrollment_per_participant on enrollments (participant_id)
  where status not in ('RELEASED','VOIDED','REFUNDED','REFUND_FAILED','NEEDS_ATTENTION','CAPTURE_FAILED');
create table payment_attempts (
  id uuid primary key default gen_random_uuid(), enrollment_id uuid not null references enrollments(id),
  attempt_no int not null, order_id text unique, authorization_id text unique, capture_id text unique, refund_id text unique,
  amount_minor bigint not null, currency char(3) not null,
  order_status text, authorization_status text, auth_expires_at timestamptz,
  capture_status text, capture_reason text, capture_issue text, capture_debug_id text,
  refund_status text, refund_reason text, mock_code text,
  created_at timestamptz not null default now(), updated_at timestamptz not null default now(),
  unique (enrollment_id, attempt_no));
create table jobs (
  id uuid primary key default gen_random_uuid(), kind text not null, idempotency_key text not null unique,
  workshop_id uuid, attempt_id uuid, payload jsonb not null default '{}',
  status text not null default 'pending' check (status in ('pending','running','succeeded','failed','unknown','needs_attention')),
  attempts int not null default 0, run_at timestamptz not null default now(),
  locked_at timestamptz, locked_by text, http_status int, provider_issue text, debug_id text, last_error text,
  created_at timestamptz not null default now(), updated_at timestamptz not null default now());
create index jobs_claim on jobs (run_at) where status = 'pending';
create table provider_events (
  id text primary key, event_type text not null, resource_type text, resource_id text,
  verified boolean not null, received_at timestamptz not null default now(), processed_at timestamptz, payload jsonb not null);
