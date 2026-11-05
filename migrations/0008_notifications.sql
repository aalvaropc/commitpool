-- A participant may leave an email to hear about their seat; notifications
-- go out through an outbox, one row per idempotency key.
alter table participants add column email text;

create table notifications (
  id uuid primary key default gen_random_uuid(),
  workshop_id uuid not null references workshops(id),
  participant_id uuid not null references participants(id),
  kind text not null,
  idempotency_key text not null unique,
  to_email text not null,
  subject text not null,
  body text not null,
  status text not null default 'pending' check (status in ('pending', 'sent', 'failed')),
  provider_message_id text,
  sent_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
create index notifications_by_workshop on notifications (workshop_id);
