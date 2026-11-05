-- An enrollment that needs an operator keeps its money status and carries a
-- flag instead: its seat and its hold stay accounted for, and the
-- one-active-enrollment index still stops a second hold. NEEDS_ATTENTION
-- stays a readable legacy status and is never written again.
alter table enrollments
  add column needs_attention_at timestamptz,
  add column attention_reason text;

update enrollments
   set needs_attention_at = updated_at, attention_reason = 'legacy NEEDS_ATTENTION status'
 where status = 'NEEDS_ATTENTION';

alter table workshops
  add constraint workshops_status_known
    check (status in ('COLLECTING','FUNDING','CONFIRMED','CANCELLED')),
  add constraint workshops_resolution_known
    check (resolution in ('OPEN','RESOLVING','SETTLED','NEEDS_ATTENTION'));

alter table enrollments
  add constraint enrollments_status_known
    check (status in ('SEAT_RESERVED','AUTHORIZED','CAPTURING','CAPTURE_PENDING','PAID','CAPTURE_FAILED',
                      'VOIDING','VOIDED','REFUNDING','REFUND_PENDING','REFUNDED','REFUND_FAILED',
                      'NEEDS_ATTENTION','RELEASED'));

create index jobs_by_attempt on jobs (attempt_id);
create index jobs_by_workshop on jobs (workshop_id);
create index jobs_unresolved on jobs (updated_at) where status in ('unknown','needs_attention');
create index enrollments_by_workshop on enrollments (workshop_id, created_at);
create index participants_by_workshop on participants (workshop_id, created_at);
create index availabilities_by_participant on availabilities (participant_id, created_at desc);
create index candidate_dates_by_workshop on candidate_dates (workshop_id, starts_at);
