CREATE TABLE oauth_grants (
 id BIGINT AUTO_INCREMENT PRIMARY KEY,
 client_uuid VARCHAR(32) NOT NULL,
 account_id BIGINT NOT NULL,
 parent_hash CHAR(64) NOT NULL,
 code_hash VARCHAR(64) NOT NULL,
 access_hash VARCHAR(64) DEFAULT NULL,
 challenge VARCHAR(43) NOT NULL,
 scope SMALLINT NOT NULL DEFAULT 0 CHECK(scope BETWEEN 0 AND 3),
 expires_at DATETIME(3) NOT NULL,
 CONSTRAINT uk_oauth_code UNIQUE(code_hash),
 CONSTRAINT uk_oauth_access UNIQUE(access_hash),
 CONSTRAINT fk_oauth_client FOREIGN KEY(client_uuid) REFERENCES oauth_clients(uuid) ON DELETE CASCADE,
 CONSTRAINT fk_oauth_parent FOREIGN KEY(parent_hash) REFERENCES tokens(access_hash) ON DELETE CASCADE,
 CONSTRAINT fk_oauth_account FOREIGN KEY(account_id) REFERENCES accounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
