-- ============================================================================
-- 003 — Multi-file deploys.
--
-- An artifact is no longer a single HTML document: a version is a BUNDLE of
-- files (index.html + css/js/images/fonts/pdf), like `glance deploy ./folder`.
--
-- Bytes live in the object store, not in Postgres, and are content-addressed:
-- the key is the sha256. Two versions sharing a logo share the blob, and a
-- redeploy that only touches index.html only uploads index.html. The CLI uses
-- the same property: it asks which hashes are missing and sends only those.
--
-- code_payload on artifact_versions stays as a text mirror of the entry
-- document: it is what the Studio opens in the editor and what the MCP tools
-- return. For a bundle it is the content of entry_path.
-- ============================================================================

-- Blob registry. No ON DELETE CASCADE towards files: a blob outlives the files
-- referencing it until the garbage collector comes by.
CREATE TABLE IF NOT EXISTS blobs (
    sha256     TEXT PRIMARY KEY,
    size_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE artifact_versions
    ADD COLUMN IF NOT EXISTS entry_path  TEXT   NOT NULL DEFAULT 'index.html',
    ADD COLUMN IF NOT EXISTS kind        TEXT   NOT NULL DEFAULT 'doc',
    ADD COLUMN IF NOT EXISTS file_count  INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_bytes BIGINT  NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'artifact_versions_kind_chk') THEN
        ALTER TABLE artifact_versions
            ADD CONSTRAINT artifact_versions_kind_chk CHECK (kind IN ('doc','bundle'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS artifact_files (
    id           BIGSERIAL PRIMARY KEY,
    version_id   BIGINT NOT NULL REFERENCES artifact_versions(id) ON DELETE CASCADE,
    -- Path relative to the bundle root, normalized: no "..", no leading slash,
    -- "/" as separator even when the deploy comes from Windows.
    path         TEXT   NOT NULL,
    content_type TEXT   NOT NULL,
    size_bytes   BIGINT NOT NULL,
    sha256       TEXT   NOT NULL REFERENCES blobs(sha256) ON DELETE RESTRICT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (version_id, path)
);
CREATE INDEX IF NOT EXISTS idx_artifact_files_sha ON artifact_files(sha256);

-- Old versions stay single documents, without rows in artifact_files: the
-- column default already classifies them as 'doc' and the content server can
-- serve both shapes.
