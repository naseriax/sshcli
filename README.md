# sshcli
sshcli is a command-line interface tool that uses the ~/.ssh/config file as a profile database. It allows you to add or remove profiles and connect to them in an interactive environment.

## Requirements to Run
 - The **ssh** and **sftp** commands must be available in your system's PATH environment variable.
 - The **sshpass** tool can be installed optionally if password authentication is needed.
 - The sshcli.db file acts as an encrypted password database since the ssh config file doesn't support storing passwords.
 - ssh passwords can be added to new or existing profiles using the --host VM10 --askpass parameters, or by choosing "Set Password" in the profile submenu.

## Supported Operating Systems:
  - In theory, all operating systems and cpu architecture are supported, but the testing is done only on MacOS.

## Installation
- MacOS (arm):
```
Run this command on terminal:

curl -fsSL https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_sshcli.sh | zsh

This script does below changes on your machine:
 - Creates a new folder: ~/sshcli
 - Downloads the sshcli binary and put it in ~/sshcli folder
 - Adds exec permission to the executable
 - Adds a new line to your ~/.zshrc file to add ~/sshcli to the PATH variable 
```
- Or you can download the latest binary for your OS/CPU architecture from the releases section manually.
  + Starting with release 20250808.0631, the new variant uses SQLite (no CGO required) to store passwords, encryption keys, notes and folder information.  
  + If you're coming from a pre-SQLite release, the app will automatically migrate your passwords, encryption key, and folder structure to the sshcli.db file during the very first execution.

## Hints
Use / to bring up the search field and find a host from the list more easily.
Use **sshcli -sql** to access the sql client engine of the sshcli.db to check the content or do some fun queries.

## Compile (Optional)
#### Requirements
To compile the code, you must have Golang installed on your system.
  - Check go.mod for the exact golang release requirement.

#### Steps:
- Install golang for your operating system and cpu architecture.
  `https://go.dev/dl/`
- Clone the repository.
  ```
  git clone https://github.com/naseriax/sshcli.git
  cd sshcli
  ```
- Run `go mod tidy` to download the required modules.
- Compile:
  - Running on Windows x86_64, compiling for Windows x86_64:
      + `$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build .`
  - Running on Apple Silicon Mac, compiling for Apple Silicon Mac:
      + `env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=date -u +.%Y%m%d.%H%M%S"`
  - Running on Apple Silicon Mac, compiling for Linux x86_64:
      + `env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=date -u +.%Y%m%d.%H%M%S"`
  - Run the tool by calling the binary.

## Usage
- Add or update a profile in the ~/.ssh/config file:
```
 # sshcli --action add --host t001 --hostname 10.10.10.10 --key '~/.ssh/id_rsa' --username root  --port 22 --askpass
 Enter Password:
```
- Remove an existing SSH profile from the ~/.ssh/config file:
```
# sshcli --action remove --host t001
```
- For more options, use the help command:
```
Usage of sshcli:
  -action string
        Action to perform: add, remove
  -askpass
        Password prompt
  -baudrate string
        BaudRate, default is 9600 (default "9600")
  -data_bits string
        databits, default is 8 (default "8")
  -device string
        device path, default is /dev/tty.usbserial-1140 (default "/dev/tty.usbserial-1140")
  -host string
        Host alias
  -hostname string
        HostName or IP address
  -httpproxy string
        http proxy to be used for the ssh/sftp over http connectivity (optional),eg. 10.10.10.10:8000
  -key string
        IdentityFile path
  -parity string
        parity, default is none (default "none")
  -port string
        Port Number
  -sql
        Direct access to the sshcli.db file to run sql queries
  -stop_bit string
        stop bit, default is 1 (default "1")
  -type string
        profile type, can be ssh or console, default is ssh (default "ssh")
  -username string
        Username
  -version
        prints the compile time (version)
```

### Significant Features
- Ability to store a note per ssh profile, encrypted and stored in the sqlite database
- `ssh` tunnel setup (-L)
- `ssh` over `http_proxy` support
- `sftp` TUI
- Zero-Touch encrypted SSH password database (using `sshpass`)
- Uses the default `~/.ssh/config` file as the profile database
- Supports console connection profiles (MacOS only, uses cu tool)
- Flat or foldered structure.
- `ping` and `tcping` integration (requires `ping` and `tcping` must be available in the cli)

### Recommended Usage
- Install the **Alacritty terminal** (works on other terminal emulators as well, but Alacritty provides the best experience).
  `https://alacritty.org/`
- Add the folder path where the `sshcli` tool exists to the **macOS `PATH` variable** by adding this line to the `~/.zshrc` file:
  `PATH=$PATH:/Path/to/the/tool/folder`
- Add the desired keyboard binding in the Alacritty config file by adding the following lines to `~/.config/alacritty/alacritty.toml`:
    - This runs the tool on **Option+S** in Alacritty.
    - The tool's executable name has been changed to `sshcli` in this example.
```
[keyboard]
  [[keyboard.bindings]]
  key = "S"
  mods = "Alt"
  chars = "sshcli\n"
```
### Some instructions (Please wait for them to load)
###### On first execution, below files/folder will be created is not created already:
```
~/.ssh folder
~/.ssh/config file
~/.ssh/sshcli.db file
```
###### On each execution, below backup files will be created
```
~/.ssh/config_lastbackup file
~/.ssh/sshcli.db_lastbackup file
```
 - A sample ssh profile will be added to the ~/.ssh/config file if the config file is empty.

![init](https://github.com/user-attachments/assets/49d03591-f4e9-4810-85bd-e588678fee6a)

- How to add a new ssh/sftp profile. Changing the host name results in creating a new profile (in the same folder) with the new name.
![add_2](https://github.com/user-attachments/assets/14fd740c-9180-448b-b9d2-a58094808a2f)

- How to edit an existing ssh/sftp profile
![edit](https://github.com/user-attachments/assets/94b49447-96d9-4846-b583-33b16fcd63ac)

- How to remove an existing ssh/sftp profile
![delete](https://github.com/user-attachments/assets/4d610185-8bcf-41bb-833c-b5e55d4c8cc7)

- How to organize ssh/sftp profiles using folders
![folder](https://github.com/user-attachments/assets/22449de7-acc2-4224-8d16-778427ad1fc7)
