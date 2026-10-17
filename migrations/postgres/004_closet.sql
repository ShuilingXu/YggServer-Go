CREATE TABLE closet (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id BIGINT NOT NULL,
    texture_id BIGINT NOT NULL,
    item_name VARCHAR(50) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ(3) NOT NULL,
    CONSTRAINT uk_closet_account_texture UNIQUE (account_id, texture_id),
    CONSTRAINT fk_closet_account FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
    CONSTRAINT fk_closet_texture FOREIGN KEY (texture_id) REFERENCES textures(id) ON DELETE CASCADE
);
