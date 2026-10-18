ALTER TABLE audit_events
 DROP CHECK audit_events_chk_1,
 ADD CONSTRAINT chk_audit_action CHECK(action IN (0,1,2,3,4,5));
