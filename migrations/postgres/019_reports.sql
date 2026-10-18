CREATE TABLE reports (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 uuid CHAR(32) NOT NULL,
 texture_id BIGINT NOT NULL,
 reporter_id BIGINT NOT NULL,
 reason VARCHAR(1000) NOT NULL,
 state SMALLINT NOT NULL DEFAULT 0 CHECK(state IN (0,1,2)),
 dedup_key BIGINT NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ(3) NOT NULL,
 handled_at TIMESTAMPTZ(3) NULL,
 handler_id BIGINT NULL,
 note VARCHAR(1000) NOT NULL DEFAULT '',
 CONSTRAINT uk_reports_uuid UNIQUE(uuid),
 CONSTRAINT uk_reports_pending UNIQUE(texture_id,reporter_id,dedup_key),
 CONSTRAINT fk_reports_texture FOREIGN KEY(texture_id) REFERENCES textures(id) ON DELETE CASCADE,
 CONSTRAINT fk_reports_reporter FOREIGN KEY(reporter_id) REFERENCES accounts(id),
 CONSTRAINT fk_reports_handler FOREIGN KEY(handler_id) REFERENCES accounts(id)
);
