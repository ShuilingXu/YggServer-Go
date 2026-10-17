CREATE TABLE closet (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    account_id BIGINT NOT NULL,
    texture_id BIGINT NOT NULL,
    item_name VARCHAR(50) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL,
    CONSTRAINT uk_closet_account_texture UNIQUE (account_id, texture_id),
    CONSTRAINT fk_closet_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_closet_texture FOREIGN KEY (texture_id) REFERENCES textures(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin;
