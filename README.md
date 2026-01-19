# sshcli
sshcli is a command-line interface tool that uses the ~/.ssh/config file as a profile database. It allows you to add or remove profiles and connect to them in an interactive environment.

## Requirements to Run
 - The **nc** on mac/**ncat** on linux/windows, **ssh** and **sftp** commands must be available in your system's PATH environment variable.
 - The **sshpass** tool can be installed optionally if password authentication is needed.
 - The sshcli.db file acts as an encrypted password database since the ssh config file doesn't support storing passwords.
 - ssh passwords can be added by choosing "Set Password" in the profile submenu.

## Supported Operating Systems:
  - In theory, all operating systems and cpu architecture are supported, but the testing is done only on MacOS.

## Installation
- MacOS (Apple Silicon - arm):
```bash
curl -fsSL https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_mac_arm.sh | zsh
```

- MacOS (Intel - x86_64):
```bash
curl -fsSL https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_mac_x86_64.sh | zsh
```

- Linux (arm):
```bash
curl -fsSL https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_linux_arm.sh | bash
```

- Linux (x86_64):
```bash
curl -fsSL https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_linux_x86_64.sh | bash
```

- Windows (arm):
```bash
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_win_arm.ps1" -UseBasicParsing | Invoke-Expression
```

- Windows (x86_64):
```bash
Invoke-WebRequest -Uri "https://raw.githubusercontent.com/naseriax/sshcli/refs/heads/main/install_scripts/install_sshcli_win_x86_64.ps1" -UseBasicParsing | Invoke-Expression
```


  - What the installation script does to your machine?
    - Creates a new folder: ~/sshcli
    - Downloads the latest sshcli binary and puts it in ~/sshcli folder
    - Adds exec permission to the executable
    - Adds a new line to your ~/.zshrc (or the OS equivalent) file to add ~/sshcli to the PATH variable 

## Hints
Use / to bring up the search field and find a host from the list more easily.
Use **sshcli -sql** to access the sql client engine of the sshcli.db to check the content or do some fun queries.
Use **sshcli -secure** to hide IPs, ports and username(Useful if sharing you screen).

## Compile (Optional)
#### Requirements
To compile the code, you must have Golang 1.25.5 installed on your system.
  - Check go.mod for the exact golang release requirement.

#### Steps:
- Install golang for your operating system and cpu architecture.
  `https://go.dev/dl/`
- Clone the repository.
  ```bash
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

- Call the binary in your terminal to see the UI
- Add or update a profile in the ~/.ssh/config file:
```bash
$> sshcli
```

- For more options, use the help command:
```bash
$> sshcli -h
Usage of sshcli:
  -action string
    	Action to perform: add, remove, only for Console profiles
  -baudrate string
    	BaudRate, default is 9600 (default "9600"),only for Console profiles
  -cleanup
    	Delete sqlite records that are not in the ssh config file
  -data_bits string
    	databits, default is 8 (default "8"),only for Console profiles
  -device string
    	device path, default is /dev/tty.usbserial-1140 (default "/dev/tty.usbserial-1140"),only for Console profiles
  -host string
    	Host alias
  -parity string
    	parity, default is none (default "none"),only for Console profiles
  -secure
    	Masks the sensitive data
  -sql
    	Direct access to the sshcli.db file to run sql queries
  -stop_bit string
    	stop bit, default is 1 (default "1"),only for Console profiles
  -version
    	prints the compile time (version)
```

### Features
- Ability to store notes per ssh profile, encrypted and stored in the sqlite database
- ssh tunnel setup (same as -L)
- ssh Socks5 tunnel (same as -D)
- ssh over `http_proxy` support
- sftp TUI
- Zero-Touch encrypted SSH password database (using `sshpass`)
- Uses the default `~/.ssh/config` file as the profile database
- Supports console connection profiles (MacOS only, uses cu tool)
- Flat or foldered structure.
- `ping` and `tcping` integration (`ping` and `tcping` must be available in the cli)

### Recommended Usage
- Install the **Alacritty terminal** (works on other terminal emulators as well, but Alacritty provides the best experience).`https://alacritty.org/`
- Add the desired keyboard binding in the Alacritty config file by adding the following lines to `~/.config/alacritty/alacritty.toml`:
    - This runs the tool on **Option+S** in Alacritty.
    - The tool's executable name has been changed to `sshcli` in this example.
```bash
[keyboard]
  [[keyboard.bindings]]
  key = "S"
  mods = "Alt"
  chars = "sshcli\n"
```
### Some instructions (Please wait for them to load)
###### On first execution, below files/folder will be created if not created already:
```bash
~/.ssh folder
~/.ssh/config file
~/.ssh/sshcli.db file
```
###### On each execution, below backup files will be created
```bash
~/.ssh/config_backup file
~/.ssh/sshcli.db_backup file
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
