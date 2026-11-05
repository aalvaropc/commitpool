-- An organizer can cancel a workshop; when and why are kept.
alter table workshops
  add column cancelled_at timestamptz,
  add column cancel_reason text;
