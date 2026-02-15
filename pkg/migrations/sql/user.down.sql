-- Drop trigger first
DROP TRIGGER IF EXISTS update_users_updated_at ON users;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column;

-- Drop index
DROP INDEX IF EXISTS users_email_unique_idx;

-- Drop table
DROP TABLE IF EXISTS users;
