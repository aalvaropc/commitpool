alter table proposals
  add column extended_at timestamptz,
  add column extended_from timestamptz,
  add constraint proposals_extension_together check ((extended_at is null) = (extended_from is null));
