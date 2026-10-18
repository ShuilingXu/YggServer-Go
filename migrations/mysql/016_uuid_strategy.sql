ALTER TABLE players ADD COLUMN uuid_strategy SMALLINT NOT NULL DEFAULT 0 CHECK (uuid_strategy IN (0,1));
