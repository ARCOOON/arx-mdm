-- Embedded PKI: enrollment no longer stores an external CA one-time token.
ALTER TABLE enrollment_tokens DROP COLUMN IF EXISTS step_sign_token;
