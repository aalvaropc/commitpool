alter table participants
  add column source text not null default 'link' check (source in ('link', 'chat', 'demo')),
  add column chat_message text check (chat_message is null or char_length(chat_message) <= 2000),
  add column chat_link_token text check (chat_link_token is null or source = 'chat');
