CREATE TABLE IF NOT EXISTS schema_migrations (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    version BIGINT NOT NULL,
    checksum CHAR(64) NOT NULL,
    dirty SMALLINT NOT NULL DEFAULT 0 CHECK (dirty IN (0, 1)),
    CONSTRAINT uk_schema_migrations_version UNIQUE (version)
) ENGINE=InnoDB;
