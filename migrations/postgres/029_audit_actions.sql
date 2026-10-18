ALTER TABLE audit_events
 DROP CONSTRAINT audit_events_action_check,
 ADD CONSTRAINT chk_audit_action CHECK(action IN (0,1,2,3,4,5));
