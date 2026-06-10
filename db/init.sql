CREATE TABLE users (
    id            SERIAL PRIMARY KEY,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name  TEXT,
    bio           TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE posts (
    id         SERIAL PRIMARY KEY,
    user_id    INT REFERENCES users(id),
    body       TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE follows (
    follower_id INT,
    followee_id INT,
    PRIMARY KEY (follower_id, followee_id)
);

-- Enable trigram extension for full-text search; guarded so a missing
-- pg_trgm does not abort init (fallback: sequential ILIKE scan).
DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS pg_trgm;
    CREATE INDEX IF NOT EXISTS posts_body_trgm_idx ON posts USING gin (body gin_trgm_ops);
EXCEPTION WHEN OTHERS THEN
    -- pg_trgm unavailable; search degrades to sequential ILIKE scan (correct, slower).
    NULL;
END $$;
