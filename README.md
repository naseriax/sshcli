# sshcli
sshcli is a command-line interface tool that uses the ~/.ssh/config file as a profile database. It allows you to add or remove profiles and connect to them in an interactive environment.

## Requirements to Compile
To compile the code, you must have Golang installed on your system.
  - Check go.mod for the exact golang release requirement.

## Requirements to Run
 - The **ssh** and **sftp** commands must be available in your system's PATH environment variable.
 - The **sshpass** tool can be installed optionally if password authentication is needed.
 - The passwords.json file acts as a password database since the ssh config file doesn't support storing passwords. You can add your clear-text passwords to the file, and the tool will encrypt them after the first execution (based on the isEncrypted value). 
 - Alternatively, ssh passwords can be added to new or existing profiles using the --host VM10 --askpass parameters, or by choosing "Set Password" in the profile submenu.

## Supported Operating Systems:
  - In theory, all operating systems and cpu architecture are supported, but the testing is done only on MacOS.

## Steps to compile
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
    `$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build .`
  - Running on Apple Silicon Mac, compiling for Apple Silicon Mac:
    `env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=date -u +.%Y%m%d.%H%M%S"`
  - Running on Apple Silicon Mac, compiling for Linux x86_64:
    `env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=date -u +.%Y%m%d.%H%M%S"`
  - Run the tool by calling the binary.

## Installation
You can download the latest binary for your OS/CPU architecture from the release section.

## Hints
Use / to bring up the search field and find a host from the list more easily.

## Usage
- Add or update a profile in the ~/.ssh/config file:
`# ./sshcli --action add --host t001 --hostname 10.10.10.10 --key '~/.ssh/id_rsa' --username root  --port 22 --askpass`
Enter Password:
- Remove an existing SSH profile from the ~/.ssh/config file:
`# ./sshcli --action remove --host t001`
- For more options, use the help command:
```
└──> sshcli -h
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
  -key string
    	IdentityFile path
  -parity string
    	parity, default is none (default "none")
  -port string
    	Port Number
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

- `ssh` over `http_proxy` support
- `sftp` TUI
- Zero-Touch Encrypted SSH Password Database (using `sshpass`)
- Uses the default `~/.ssh/config` file as the profile database
- Supports console connection profiles
- Flat or Folder structure, where the folder structure is saved in a separate JSON file. (Changes to flat if the file is removed)
- `ping` and `tcping` integration (requires `ping` and `tcping` to be available in the cli)

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

