# Changelog
#### 20250812.0727
  - Notes are now stored in the sqlite database as encrypted instead of clear text
#### 20250811.0922
  - Password encryption key is now migrated and stored in the sqlite database
  - Passwords.json file is now migrated and stored in the sqlite database
  - Folder structure is now migrated and stored in the sqlite database
  - Console.json file is moved to sqlite (not migrated)
  - It's possible to access the sqlite file directly via `sshcli -sql` for debugging. No dependency
  - User can create notes per ssh profile and store in the sqlite database
  - Ssh profiles with note or applied https_proxy can be identified by the extra flags on each profile
  - User can push the public key string to the remote host using ssh-copy-id tool (must be availble on the shell)
  - If a ssh profile is duplicated using "Duplicate/Edit profile" option followed by changing the host, the new profile is placed in the same folder as the original one 
  - Added a more secure password encryption/decryption algorithm cipher.NewGCM(block) instead of cipher.NewCFBDecrypter(block, iv). Due to backward incompatibility, the old algorithm is still there to decrypt the migrated passwords.
