-- Phase 6: ticket priority, resolution markdown body, per-prefix sequences.
ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'normal';

ALTER TABLE resolutions
    ADD COLUMN IF NOT EXISTS markdown TEXT NOT NULL DEFAULT '';

CREATE SEQUENCE IF NOT EXISTS ticket_seq_inc AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_req AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_chg AS bigint START 1 INCREMENT 1;
CREATE SEQUENCE IF NOT EXISTS ticket_seq_prj AS bigint START 1 INCREMENT 1;
