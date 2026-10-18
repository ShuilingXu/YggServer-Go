CREATE TABLE external_identities (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 uuid CHAR(32) NOT NULL,
 account_id BIGINT NOT NULL,
 issuer VARCHAR(255) NOT NULL,
 subject VARCHAR(255) NOT NULL,
 created_at TIMESTAMPTZ(3) NOT NULL,
 CONSTRAINT uk_external_uuid UNIQUE(uuid),
 CONSTRAINT uk_external_subject UNIQUE(issuer,subject),
 CONSTRAINT fk_external_account FOREIGN KEY(account_id) REFERENCES accounts(id)
);
