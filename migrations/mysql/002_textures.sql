CREATE TABLE textures (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    uuid CHAR(32) NOT NULL,
    account_id BIGINT NOT NULL,
    name VARCHAR(50) NOT NULL,
    type SMALLINT NOT NULL DEFAULT 0 CHECK (type IN (0, 1, 2)),
    hash CHAR(64) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    is_public SMALLINT NOT NULL DEFAULT 0 CHECK (is_public IN (0, 1)),
    created_at DATETIME(3) NOT NULL,
    CONSTRAINT uk_textures_uuid UNIQUE (uuid),
    CONSTRAINT fk_textures_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
