INSERT INTO point_entries(account_id,delta,kind,created_at) SELECT id,1000,0,created_at FROM accounts;
