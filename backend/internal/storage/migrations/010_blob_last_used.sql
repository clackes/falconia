-- 010: when a blob was last wanted.
--
-- The garbage collector used to judge a blob by its age alone. A blob that had
-- been orphaned for a day, and that a deploy in progress had just been told
-- "you already have it", looked exactly as collectable as one nobody wanted:
-- deleting it in that window broke the deploy. last_used_at is refreshed every
-- time a client asks about the blob or uploads it again, and the collector
-- only considers blobs nobody has wanted for a while.
ALTER TABLE blobs ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE blobs SET last_used_at = created_at;
CREATE INDEX IF NOT EXISTS idx_blobs_last_used ON blobs(last_used_at);

-- File sizes used to be taken from the deploy manifest, that is from the
-- client, and the content server sends them as Content-Length. A wrong one
-- truncates the response or makes the browser hang waiting for bytes that
-- never come. The blob row holds the size measured when the bytes were
-- hashed: align every file on it.
UPDATE artifact_files f
   SET size_bytes = b.size_bytes
  FROM blobs b
 WHERE f.sha256 = b.sha256 AND f.size_bytes <> b.size_bytes;
