create table open_approvals (
  id uuid primary key default gen_random_uuid(),
  workshop_id uuid not null references workshops(id),
  date_id uuid not null references candidate_dates(id),
  terms_hash bytea not null,
  price_minor bigint not null,
  currency char(3) not null,
  minimum int not null,
  capacity int not null,
  payment_window_min int not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  approved_at timestamptz,
  used_at timestamptz,
  check (expires_at > created_at),
  check (used_at is null or approved_at is not null)
);
create index open_approvals_by_workshop on open_approvals (workshop_id, created_at);
