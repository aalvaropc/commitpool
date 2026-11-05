-- An operator can retry a refund that failed; the failed refunds' ids are
-- kept on the attempt.
alter table payment_attempts add column failed_refund_ids text[] not null default '{}';
