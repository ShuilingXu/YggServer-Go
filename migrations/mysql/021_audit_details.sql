ALTER TABLE audit_events
 ADD COLUMN result SMALLINT NOT NULL DEFAULT 0 CHECK(result IN (0,1)),
 ADD COLUMN target_state SMALLINT NOT NULL DEFAULT -1 CHECK(target_state IN (-1,0,1,2)),
 ADD COLUMN target_permission SMALLINT NOT NULL DEFAULT -1 CHECK(target_permission IN (-1,0,1,2));
