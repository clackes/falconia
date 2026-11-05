-- 011: deleting an artifact that someone forked.
--
-- fork_relations pointed at the parent with ON DELETE RESTRICT, so the owner
-- of a popular artifact could not delete it any more: every fork anyone had
-- made held it hostage, and the delete failed with a 500. A fork is an
-- independent copy from birth; when its parent goes away it should simply stop
-- knowing where it came from. The relation row is the only thing removed: the
-- forked artifact itself is untouched.
ALTER TABLE fork_relations DROP CONSTRAINT IF EXISTS fork_relations_parent_artifact_id_fkey;
ALTER TABLE fork_relations
    ADD CONSTRAINT fork_relations_parent_artifact_id_fkey
    FOREIGN KEY (parent_artifact_id) REFERENCES artifacts(id) ON DELETE CASCADE;
