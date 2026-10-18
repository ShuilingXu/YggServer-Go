CREATE UNIQUE INDEX uk_players_name_folded ON players ((LOWER(name)));
