CREATE TABLE recovery_tokens (
 id BIGINT AUTO_INCREMENT PRIMARY KEY,
 account_id BIGINT NOT NULL,
 token_hash CHAR(64) NOT NULL,
 email VARCHAR(255) NOT NULL,
 kind SMALLINT NOT NULL DEFAULT 0 CHECK (kind IN (0,1)),
 created_at DATETIME(3) NOT NULL,
 expires_at DATETIME(3) NOT NULL,
 CONSTRAINT uk_recovery_hash UNIQUE(token_hash),
 CONSTRAINT uk_recovery_account_kind UNIQUE(account_id,kind),
 CONSTRAINT fk_recovery_account FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
