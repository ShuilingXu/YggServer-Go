ALTER TABLE audit_events DROP CHECK chk_audit_action,
 ADD CONSTRAINT chk_audit_action CHECK(action IN (0,1,2,3,4,5,6,7,8,9));
