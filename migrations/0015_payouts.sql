alter table workshops
  add column payout_email text,
  add column payout_locked_at timestamptz;

create table payouts (
  id uuid primary key default gen_random_uuid(),
  workshop_id uuid not null unique references workshops(id),
  sender_batch_id text not null unique,
  receiver text not null,
  amount_minor bigint not null check (amount_minor > 0),
  gross_minor bigint not null check (gross_minor >= amount_minor),
  fee_minor bigint not null check (fee_minor >= 0),
  currency char(3) not null,
  seats int not null check (seats > 0),
  early boolean not null default false,
  status text not null default 'created' check (status in ('created', 'sent', 'succeeded', 'unclaimed', 'failed', 'unknown')),
  payout_batch_id text unique,
  batch_status text,
  payout_item_id text unique,
  item_status text,
  transaction_id text,
  issue text,
  debug_id text,
  created_at timestamptz not null default now(),
  sent_at timestamptz,
  checked_at timestamptz,
  completed_at timestamptz,
  updated_at timestamptz not null default now(),
  check (gross_minor = amount_minor + fee_minor)
);

create index payouts_open on payouts (status) where status in ('created', 'sent', 'unclaimed', 'unknown');

alter table payment_attempts add column payout_id uuid references payouts(id);
create index payment_attempts_by_payout on payment_attempts (payout_id) where payout_id is not null;
