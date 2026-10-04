-- ============================================================================
-- 009 — "Team" access only exists in team spaces.
--
-- Databases that predate spaces translated is_public = TRUE into 'org', even
-- for artifacts that ended up in a personal space. A personal space has no
-- team, so those artifacts were already visible to their owner only; this
-- makes the stored value say so, and the Share dialog show it truthfully.
--
-- Deliberately NOT converted to 'link': silently widening access to "anyone
-- with the link" would be the wrong default. Owners can share them by link in
-- one click.
-- ============================================================================

UPDATE artifacts a
   SET visibility = 'private'
  FROM spaces s
 WHERE s.id = a.space_id AND s.kind = 'personal' AND a.visibility = 'org';
