-- Usherr: linked screenings, step 5 (home_link_rewards.go, home_rewards.go,
-- internal/link/rewards): rewards. Ticket stubs and stamps over the link,
-- collectible stubs, and badges.

-- Each stub's number ("No. 0047"): the host's running count of the stubs it
-- handed out. A stub from a linked Usherr keeps the number its host gave it.
-- sid: the linked screening it was for ('' for a local party); issuer: the
-- linked Usherr that handed it out ('' for this one). runtime: the film's
-- minutes (0 when unknown), for when the night ended. agreed_ms: the host
-- confirmed the stamp; reported_ms: the stamp was last sent to the host.
-- hosted: the person hosted that party (the House lights badge).
ALTER TABLE community_stubs ADD COLUMN serial INTEGER NOT NULL DEFAULT 0;
ALTER TABLE community_stubs ADD COLUMN hosted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE community_stubs ADD COLUMN sid TEXT NOT NULL DEFAULT '';
ALTER TABLE community_stubs ADD COLUMN issuer TEXT NOT NULL DEFAULT '';
ALTER TABLE community_stubs ADD COLUMN runtime INTEGER NOT NULL DEFAULT 0;
ALTER TABLE community_stubs ADD COLUMN agreed_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE community_stubs ADD COLUMN reported_ms INTEGER NOT NULL DEFAULT 0;
UPDATE community_stubs SET serial = rowid, hosted = (seat = 0);
CREATE INDEX community_stubs_sid ON community_stubs (sid, issuer);

-- Host side: the seats handed to a linked Usherr's people at a screening
-- hosted here (a block each time more of them come), with the first stub
-- number; sent_ms when the stubs reached the relay (0: still to send).
CREATE TABLE reward_seats (
    showtime_id INTEGER NOT NULL,
    first_seat  INTEGER NOT NULL,
    count       INTEGER NOT NULL,
    peer_id     INTEGER NOT NULL REFERENCES link_peers(id) ON DELETE CASCADE,
    first_no    INTEGER NOT NULL,
    sent_ms     INTEGER NOT NULL DEFAULT 0,
    created_ms  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (showtime_id, first_seat)
);
CREATE INDEX reward_seats_peer ON reward_seats (peer_id);

-- Host side: the seats a linked Usherr said were stamped (its people stayed
-- to the end), as the host keeps them.
CREATE TABLE reward_stamps (
    showtime_id INTEGER NOT NULL,
    seat        INTEGER NOT NULL,
    peer_id     INTEGER NOT NULL REFERENCES link_peers(id) ON DELETE CASCADE,
    stamped_ms  INTEGER NOT NULL,
    PRIMARY KEY (showtime_id, seat)
);

-- Each party someone was invited to (they answered, or got a stub), kept
-- after the party is gone, for the Perfect attendance badge. person is
-- 'here:<account id>' or 'discord:<Discord id>'; party is the showtime here.
CREATE TABLE reward_invites (
    person      TEXT    NOT NULL,
    showtime_id INTEGER NOT NULL,
    at_ms       INTEGER NOT NULL,
    PRIMARY KEY (person, showtime_id)
);
CREATE INDEX reward_invites_when ON reward_invites (person, at_ms);
