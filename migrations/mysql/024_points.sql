CREATE TABLE point_entries (
 id BIGINT AUTO_INCREMENT PRIMARY KEY,
 account_id BIGINT NOT NULL,
 delta BIGINT NOT NULL,
 kind SMALLINT NOT NULL DEFAULT 0,
 created_at DATETIME(3) NOT NULL,
 CONSTRAINT fk_points_account FOREIGN KEY(account_id) REFERENCES accounts(id)
);
