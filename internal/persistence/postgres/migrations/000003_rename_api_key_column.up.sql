-- Rename api_key_encrypted to api_key_value per DECISIONS #120.
-- Storage is restricted plaintext, not encrypted material.
-- Idempotent: skip rename if old column no longer exists (tests reuse the schema).
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'gorouter_providers'
          AND column_name = 'api_key_encrypted'
    ) THEN
        ALTER TABLE gorouter_providers RENAME COLUMN api_key_encrypted TO api_key_value;
    END IF;
END $$;
