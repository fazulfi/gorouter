-- Rename api_key_encrypted to api_key_value per DECISIONS #120.
-- Storage is restricted plaintext, not encrypted material.
ALTER TABLE gorouter_providers RENAME COLUMN api_key_encrypted TO api_key_value;
