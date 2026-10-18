CREATE TABLE external_identities (
 id BIGINT AUTO_INCREMENT PRIMARY KEY,
 uuid CHAR(32) NOT NULL,
 account_id BIGINT NOT NULL,
 issuer VARCHAR(255) NOT NULL,
 subject VARCHAR(255) NOT NULL,
 created_at DATETIME(3) NOT NULL,
 CONSTRAINT uk_external_uuid UNIQUE(uuid),
 CONSTRAINT uk_external_subject UNIQUE(issuer,subject),
 CONSTRAINT fk_external_account FOREIGN KEY(account_id) REFERENCES accounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
