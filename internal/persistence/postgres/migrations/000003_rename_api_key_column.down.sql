-- Reverse rename: restore api_key_encrypted column name.
ALTER TABLE gorouter_providers RENAME COLUMN api_key_value TO api_key_encrypted;
