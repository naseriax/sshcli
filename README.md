
sshcli is a command-line interface tool that utilizes the `~/.ssh/config` file as a profile database. It allows you to add or remove profiles and connect to those profiles in an interactive environment.

## Requirements to compile:

To compile the code, you must have Golang installed on your system.

## Requirements to run:
```
 - ssh and sftp commands must be available in the path environment.
 - sshpass tool can be installed optionally if password authentication is needed.
 - The passwords.json file acts as a password database since the ssh config file doesn't support storing passwords. you can add your clear-text passwords to to file and the tool will encrypt them (based on isEncrypted value) after the first execution. Alternatively, ssh passwords can be added to the new or existing profiles using "-host VM10 --askpass" parameters.
 - To make the best use of the tool in Windows OS, run it in the Windows Terminal app.

```
### Steps:
```
1: Clone the repository.
2: Run "go mod tidy" to download the extra modules.
3: How to compile:
  + Running on windows , compiling for windows:
    - `$env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build .`
  + running on Apple Silicon , compiling for Mac:
    - `env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=`date -u +.%Y%m%d.%H%M%S`"`
  + running on Apple Silicon , compiling for linux x86_64:
    - `env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=`date -u +.%Y%m%d.%H%M%S`"`
4: Add the binary to the path varible (Optional).
5: Fill ~/.ssh/config file and add the target credentials.
6: Run the tool by calling the binary.
```

## Installation:
You can download the latest binary from the release section for your OS/CPU architecture.

## Hints

Use `/` to bring up the search field to find the host from the list more easily.

## Usage:

### Bring up the connections UI:

```
# ./sshcli
```

### Add or update a profile in the ~/.ssh/config file:

```
# ./sshcli --action add --host t001 --hostname 10.10.10.10 --key '~/.ssh/id_rsa' --user root  --port 22 --askpass
Enter Password: 
```

### Remove an existing SSH profile from the ~/.ssh/config file:

```
# ./sshcli --action remove --host t001
```

### For more options, use the help command:

```
 ~ ➤ sshcli -h
Usage of sshcli:
  -action string
    	Action to perform: add, remove ==> add can be used for both adding and updating
  -host string
    	Host alias
  -hostname string
    	HostName or IP address
  -key string
    	IdentityFile path
  -askpass                           ==> Thi triggers the secure password prompt
    	Password prompt
  -port string
    	Port Number
  -username string
    	Username
  -version
    	prints the compile time (version)
```

<img width="619" alt="image" src="https://github.com/user-attachments/assets/4e864ef1-2792-46b4-85fb-6cc4383b245d">

<img width="451" alt="image" src="https://github.com/user-attachments/assets/051f70aa-c82a-4630-bcd4-b7419b391d05">
