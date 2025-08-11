# Changelog
#### up to 20250811.0922
  - Password encryption key is now migrated and stored in the sqlite database
  - Passwords.json file is now migrated and stored in the sqlite database
  - Folder structure is not migrated and stored in the sqlite database
  - Console.json file is moved to sqlite (not migrated)
  - It's possible to access the sqlite file ditectly via sshcli -sql for debugging. No dependency
  - User can create notes per ssh profile and store in the sqlite database
  - Ssh profiles with note or applied http_proxy can be identified by the extra flags on each profile
  - User can push the public key string to the remote host using ssh-copy-id tool (must be availble on the shell)
  - If a ssh prodile is duplicated using Duplicate/Edit profile option followed by changing the host, the new profile is placed in the same folder as the original one.
