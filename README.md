# sshcli

## Small cli tool to select ssh/sftp profiles from ~/.ssh/config file using keyboard arrow keys and connect

# How to run:
## Requirement:
```
Golang must be installed on the system where you want to compile the code.
```
## Steps:
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

# Usage:
```
Usage of sshcli:
  -V	prints the compile time
  -action string
    	Action to perform: add or remove
  -host string
    	Host alias
  -hostname string
    	HostName
  -key string
    	IdentityFile path
  -port string
    	Port
  -user string
    	User
```


