# SSHCLI

SSHCLI is a command-line interface tool that utilizes the `~/.ssh/config` file as a profile database. It allows you to add or remove profiles and connect to those profiles in an interactive environment.

## Requirements to compile:

To compile the code, you must have Golang installed on your system.

## Requirements to run:

ssh and sftp commands must be available in the path environment.

### Steps:
```
1: clone the repository.
2: run "go mod tidy" to download the extra modules.
3: running on windows , compiling for windows ==> $env:GOOS="windows"; $env:GOARCH="amd64"; $env:CGO_ENABLED="0"; go build .
   running on Apple Silicon , compiling for Mac ==> env GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=`date -u +.%Y%m%d.%H%M%S`"
   running on Apple Silicon , compiling for linux x86_64 ==> env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.CompileTime=`date -u +.%Y%m%d.%H%M%S`"
4: add the binary to the path varible.
5: fill ~/.ssh/config file and add the target credentials.
6: run the tool by calling the binary.
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
# ./sshcli -action add -host t001 -hostname 10.10.10.10 -key '~/.ssh/id_rsa' -user root -port 22
```

### Remove an existing SSH profile from the ~/.ssh/config file:

```
# ./sshcli -action remove -host t001
```

### For more options, use the help command:

```
 ~ ➤ sshcli -h
Usage of sshcli:
  -A, --action string
    	Action to perform: add, remove
  -H, --host string
    	Host alias
  -I, --hostname string
    	HostName or IP address
  -K, --key string
    	IdentityFile path
  -P, --port string
    	Port Number
  -U, --username string
    	Username
  -V	prints the compile time
```
