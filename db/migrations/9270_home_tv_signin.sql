-- Usherr: Sign in on a TV (home_tv_signin.go). A session started by approving a
-- TV's code on a phone keeps the kind of device it was, so Signed-in devices can
-- say "Chrome on Android TV" instead of just how it signed in. Empty for every
-- other session. The codes themselves live only in memory for five minutes.

ALTER TABLE sessions ADD COLUMN device TEXT NOT NULL DEFAULT '';
