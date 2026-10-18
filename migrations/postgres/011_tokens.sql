CREATE TABLE tokens (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id BIGINT NOT NULL,
    access_hash CHAR(64) NOT NULL,
    client_hash CHAR(64) NOT NULL,
    player_uuid CHAR(32) NULL,
    kind SMALLINT NOT NULL DEFAULT 0 CHECK (kind IN (0, 1)),
    created_at TIMESTAMPTZ(3) NOT NULL,
    expires_at TIMESTAMPTZ(3) NOT NULL,
    refresh_until TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT uk_tokens_access_hash UNIQUE (access_hash),
    CONSTRAINT fk_tokens_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_tokens_player FOREIGN KEY (player_uuid) REFERENCES players(uuid) ON DELETE CASCADE
);
