-- Reverse rename: restore api_key_encrypted column name.
-- Idempotent: skip rename if the new column no longer exists (tests reuse the schema).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'gorouter_providers'
          AND column_name = 'api_key_value'
    ) THEN
        ALTER TABLE gorouter_providers RENAME COLUMN api_key_value TO api_key_encrypted;
    END IF;
END $$;
