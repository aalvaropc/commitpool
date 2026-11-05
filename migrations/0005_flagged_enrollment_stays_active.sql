-- A flagged enrollment stays the participant's one active enrollment
-- whatever its status: its money state is uncertain, so no second hold.
drop index one_active_enrollment_per_participant;
create unique index one_active_enrollment_per_participant on enrollments (participant_id)
  where status not in ('RELEASED','VOIDED','REFUNDED','REFUND_FAILED','NEEDS_ATTENTION','CAPTURE_FAILED')
     or needs_attention_at is not null;
