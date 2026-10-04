-- ============================================================================
-- 007 — A slug for artifacts that have none.
--
-- The slug was optional in the old schema, and artifacts without one ended up
-- with a UUID in their address. It works, but nobody enjoys pasting it in a
-- chat, and pasting it in a chat is the point of the product.
--
-- It is derived from the title, reduced to the URL alphabet. On a collision
-- within the same space a counter is appended, because uniqueness is per space
-- and two teams can both have a "roadmap".
-- ============================================================================

DO $$
DECLARE
    a          RECORD;
    base_slug  TEXT;
    try_slug   TEXT;
    n          INTEGER;
BEGIN
    FOR a IN
        SELECT id, space_id, title FROM artifacts
        WHERE slug IS NULL OR slug = ''
        ORDER BY created_at
    LOOP
        base_slug := regexp_replace(LOWER(a.title), '[^a-z0-9]+', '-', 'g');
        base_slug := trim(BOTH '-' FROM base_slug);
        -- A title made only of emoji or non-Latin characters reduces to
        -- nothing: a generic name beats an empty address.
        IF base_slug = '' THEN
            base_slug := 'artifact';
        END IF;
        base_slug := LEFT(base_slug, 60);

        try_slug := base_slug;
        n := 1;
        WHILE EXISTS (
            SELECT 1 FROM artifacts
             WHERE space_id = a.space_id AND LOWER(slug) = try_slug
        ) LOOP
            n := n + 1;
            try_slug := base_slug || '-' || n;
        END LOOP;

        UPDATE artifacts SET slug = try_slug WHERE id = a.id;
    END LOOP;
END $$;
