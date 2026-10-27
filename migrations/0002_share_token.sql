-- The owner can show the share link again: the share token is meant to be
-- passed around, so it is kept in plaintext next to its hash. The owner and
-- participant tokens are still stored only as hashes.
alter table workshops add column share_token text;
