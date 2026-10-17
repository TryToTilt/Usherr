-- Usherr: the NSFW 18+ module is called nsfw, not scenes, and an item in it a
-- video. Its tables, settings, activity, blocklist entries and problem links
-- move to the new names; nothing in them is lost.

ALTER TABLE scenes RENAME TO nsfw_videos;
ALTER TABLE scene_performers RENAME TO nsfw_performers;
ALTER TABLE scene_studios RENAME TO nsfw_studios;
ALTER TABLE scene_downloads RENAME TO nsfw_downloads;
ALTER TABLE nsfw_downloads RENAME COLUMN scene_id TO video_id;

DROP INDEX IF EXISTS scenes_wanted;
DROP INDEX IF EXISTS scenes_studio_date;
DROP INDEX IF EXISTS scene_downloads_scene;
CREATE INDEX nsfw_videos_wanted ON nsfw_videos(wanted, status);
CREATE INDEX nsfw_videos_studio_date ON nsfw_videos(studio_stash_id, release_date);
CREATE INDEX nsfw_downloads_video ON nsfw_downloads(video_id);

UPDATE settings SET key = 'nsfw.' || substr(key, 8) WHERE key LIKE 'scenes.%';
UPDATE settings SET value = replace(replace(value, '"scenes.search"', '"nsfw.search"'), '"scenes.follow"', '"nsfw.follow"') WHERE key = 'home.schedules';
UPDATE activity SET event_type = 'nsfw-' || substr(event_type, 7) WHERE event_type LIKE 'scene-%';
UPDATE blocklist SET reason = '[nsfw] ' || substr(reason, 10) WHERE reason LIKE '[scenes] %';
UPDATE problems SET link = '/nsfw' || substr(link, 8) WHERE link LIKE '/scenes%';
