ALTER TABLE players DROP CHECK players_chk_1,
 ADD CONSTRAINT chk_players_uuid_strategy CHECK(uuid_strategy IN (0,1,2));
