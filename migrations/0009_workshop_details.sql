alter table workshops
    add column host_name text not null default '',
    add column location text not null default '',
    add column venue_cost_minor bigint check (venue_cost_minor >= 0);
