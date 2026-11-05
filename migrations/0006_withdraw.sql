-- A participant can give their seat up before being charged; when is kept,
-- and a withdrawn enrollment never takes a seat again.
alter table enrollments add column withdrawn_at timestamptz;
