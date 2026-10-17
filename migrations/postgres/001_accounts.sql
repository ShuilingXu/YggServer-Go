CREATE TABLE accounts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid CHAR(32) NOT NULL,
    email VARCHAR(255) NOT NULL,
    password_hash BYTEA NOT NULL,
    state SMALLINT NOT NULL DEFAULT 0 CHECK (state IN (0, 1, 2)),
    permission SMALLINT NOT NULL DEFAULT 0 CHECK (permission IN (0, 1, 2)),
    verified SMALLINT NOT NULL DEFAULT 0 CHECK (verified IN (0, 1)),
    created_at TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT uk_accounts_uuid UNIQUE (uuid),
    CONSTRAINT uk_accounts_email UNIQUE (email)
);
