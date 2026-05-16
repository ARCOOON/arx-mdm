-- Phase 38: Policy merge metadata on profile assignments.

ALTER TABLE profile_assignments
    ADD COLUMN assignment_state TEXT NOT NULL DEFAULT 'ok';

ALTER TABLE profile_assignments
    ADD CONSTRAINT profile_assignments_state_chk CHECK (
        assignment_state IN ('ok', 'conflict')
    );

COMMENT ON COLUMN profile_assignments.assignment_state IS 'ok when source profiles agree on merged keys for affected devices; conflict when merge detected divergent explicit values that required restrictive resolution.';
