ALTER TABLE players DROP CONSTRAINT players_uuid_strategy_check,
 ADD CONSTRAINT chk_players_uuid_strategy CHECK(uuid_strategy IN (0,1,2));
