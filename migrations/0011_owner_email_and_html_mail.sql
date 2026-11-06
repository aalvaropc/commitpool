alter table workshops add column owner_email text;

alter table notifications alter column participant_id drop not null;
alter table notifications add column html text not null default '';

alter table notifications drop constraint notifications_status_check;
alter table notifications add constraint notifications_status_check check (status in ('pending', 'sent', 'failed', 'skipped'));
