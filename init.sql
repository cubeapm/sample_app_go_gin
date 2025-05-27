-- Initialize the database with sample data
USE testdb;

-- The users table will be auto-created by GORM migration
-- This file is optional and can be used for additional setup

-- Grant permissions (redundant but safe)
GRANT ALL PRIVILEGES ON testdb.* TO 'root'@'%';
GRANT ALL PRIVILEGES ON testdb.* TO 'appuser'@'%';
FLUSH PRIVILEGES;

-- Optional: Insert sample data after table creation
-- Note: GORM will handle table creation, so we'll wait for that
-- These inserts can be run manually after the application starts

-- Example insert statements (commented out as GORM handles this):
-- INSERT INTO users (name, email, created_at, updated_at) VALUES 
--   ('John Doe', 'john@example.com', NOW(), NOW()),
--   ('Jane Smith', 'jane@example.com', NOW(), NOW()),
--   ('Bob Johnson', 'bob@example.com', NOW(), NOW());

-- Create additional indexes if needed
-- CREATE INDEX idx_users_email ON users(email);
-- CREATE INDEX idx_users_created_at ON users(created_at); 