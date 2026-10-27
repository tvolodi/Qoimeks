-- Feedback from the app (kind 'user') and, later, errors the server recorded itself
-- ('speech_error', 'llm_error', 'post_error'). Append-only like the ledger.
create table feedback (
    id              bigint generated always as identity primary key,
    created_at      timestamptz not null default now(),
    kind            text not null,
    text            text not null,
    voice_text      text,
    app_version     text not null default '',
    android_version text not null default '',
    device          text not null default '',
    operator        text not null default '',
    screen          text not null default '',
    errors          jsonb not null default '[]',
    provider        text
);

create trigger feedback_append_only before update or delete or truncate on feedback
    for each statement execute function forbid_change();
