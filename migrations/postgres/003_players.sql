CREATE TABLE players (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    uuid CHAR(32) NOT NULL,
    account_id BIGINT NOT NULL,
    name VARCHAR(16) NOT NULL,
    skin_id BIGINT NULL,
    cape_id BIGINT NULL,
    created_at TIMESTAMPTZ(3) NOT NULL,
    updated_at TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT uk_players_uuid UNIQUE (uuid),
    CONSTRAINT uk_players_name UNIQUE (name),
    CONSTRAINT fk_players_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_players_skin FOREIGN KEY (skin_id) REFERENCES textures(id) ON DELETE SET NULL,
    CONSTRAINT fk_players_cape FOREIGN KEY (cape_id) REFERENCES textures(id) ON DELETE SET NULL
);
