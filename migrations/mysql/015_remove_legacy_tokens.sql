DELETE FROM tokens WHERE OCTET_LENGTH(client_cipher)=0;
