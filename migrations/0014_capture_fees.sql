alter table payment_attempts
  add column capture_fee_minor bigint,
  add column capture_net_minor bigint,
  add constraint payment_attempts_capture_net_not_negative check (capture_net_minor is null or capture_net_minor >= 0),
  add constraint payment_attempts_capture_fees_together check ((capture_fee_minor is null) = (capture_net_minor is null));
