-- Usherr: Service checks (internal/servicecheck). Addresses Usherr asks every
-- so often (Jellyfin, a download client, anything with a web address), with a
-- message through the notification targets when one stops answering and when
-- it answers again. It does the job of a separate watchdog container.
--
-- expect: '200' wants exactly 200, 'ok' any 2xx or 3xx answer, 'any' any
-- answer at all (a login page that says 401 still means it is running).
-- The state columns are the last word on each service: state 'unknown' until
-- the first answer, then 'up' or 'down'; fails counts the failed checks in a
-- row; alerted is 1 once the "down" message for this outage went out.
CREATE TABLE service_checks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL,
    url           TEXT    NOT NULL,
    expect        TEXT    NOT NULL DEFAULT 'ok',
    keyword       TEXT    NOT NULL DEFAULT '',
    interval_sec  INTEGER NOT NULL DEFAULT 60,
    fail_after    INTEGER NOT NULL DEFAULT 3,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_ms    INTEGER NOT NULL DEFAULT 0,
    state         TEXT    NOT NULL DEFAULT 'unknown',
    fails         INTEGER NOT NULL DEFAULT 0,
    fail_since_ms INTEGER NOT NULL DEFAULT 0,
    alerted       INTEGER NOT NULL DEFAULT 0,
    changed_ms    INTEGER NOT NULL DEFAULT 0,
    checked_ms    INTEGER NOT NULL DEFAULT 0,
    last_ms       INTEGER NOT NULL DEFAULT 0,
    last_code     INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT    NOT NULL DEFAULT ''
);

-- One row per check, kept for a week: the uptime and response times.
CREATE TABLE service_check_results (
    check_id INTEGER NOT NULL REFERENCES service_checks(id) ON DELETE CASCADE,
    at_ms    INTEGER NOT NULL,
    ok       INTEGER NOT NULL,
    ms       INTEGER NOT NULL DEFAULT 0,
    code     INTEGER NOT NULL DEFAULT 0,
    error    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX service_check_results_by_check ON service_check_results(check_id, at_ms);

-- When a service went down or came back, kept for 90 days.
CREATE TABLE service_check_changes (
    check_id INTEGER NOT NULL REFERENCES service_checks(id) ON DELETE CASCADE,
    at_ms    INTEGER NOT NULL,
    state    TEXT    NOT NULL,
    detail   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX service_check_changes_by_check ON service_check_changes(check_id, at_ms);
