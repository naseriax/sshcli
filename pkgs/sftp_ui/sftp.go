package sftp_ui

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/pkg/sftp"
	"github.com/rivo/tview"
	"golang.org/x/crypto/ssh"
)

type FileSystem struct {
	currentPath string
	list        *tview.List
	isRemote    bool
	sftpClient  *sftp.Client
}

func NewFileSystem(isRemote bool, sftpClient *sftp.Client) *FileSystem {
	fs := &FileSystem{
		currentPath: "/",
		list:        tview.NewList().ShowSecondaryText(false),
		isRemote:    isRemote,
		sftpClient:  sftpClient,
	}
	fs.list.SetBorder(true).SetTitle(fmt.Sprintf("%s File System", getSystemType(isRemote)))
	return fs
}

func addFileItem(list *tview.List, name string, isDir bool, isSymlink bool) {
	var suffix = ""
	var icon string
	var colorTag string
	if isSymlink {
		icon = "🔗"
		colorTag = "[yellow]"
	} else if isDir {
		icon = "📁"
		colorTag = "[blue]"
		suffix = "/"
	} else {
		icon = "📄"
		colorTag = "[white]"
	}
	list.AddItem(fmt.Sprintf("%s%s %s%s", colorTag, icon, name, suffix), "", 0, nil)
}

func getSystemType(isRemote bool) string {
	if isRemote {
		return "Remote"
	}
	return "Local"
}

func (fs *FileSystem) updateList() {
	fs.list.Clear()
	fs.list.AddItem("📁 ..", "Go to parent directory", 0, nil)

	if fs.isRemote {
		files, err := fs.sftpClient.ReadDir(fs.currentPath)
		if err != nil {
			log.Printf("Error reading remote directory: %v", err)
			return
		}
		for _, file := range files {
			addFileItem(fs.list, file.Name(), file.IsDir(), file.Mode()&os.ModeSymlink != 0)
		}
	} else {
		entries, err := os.ReadDir(fs.currentPath)
		if err != nil {
			log.Printf("Error reading local directory: %v", err)
			return
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				log.Printf("Error getting file info: %v", err)
				continue
			}
			addFileItem(fs.list, entry.Name(), entry.IsDir(), info.Mode()&os.ModeSymlink != 0)
		}
	}
}

func (fs *FileSystem) navigateTo(path string) {
	fs.currentPath = path
	fs.updateList()
}

func publicKeyFile(file string) ssh.AuthMethod {
	buffer, err := os.ReadFile(file)
	if err != nil {
		return nil
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil
	}
	return ssh.PublicKeys(key)
}

func createSFTPClient(host, user, key, password string) (*sftp.Client, error) {
	config := &ssh.ClientConfig{
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	authMethods := []ssh.AuthMethod{}

	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	if key != "" {
		authMethods = append(authMethods, publicKeyFile(key))
	}

	config.User = user
	config.Auth = authMethods

	conn, err := ssh.Dial("tcp", host, config)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %v", err)
	}

	client, err := sftp.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to create SFTP client: %v", err)
	}

	return client, nil
}

func transferFile(sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView) {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var sourceFile io.ReadCloser
	var targetFile io.WriteCloser
	var err error

	// Open source file
	if sourceFS.isRemote {
		sourceFile, err = sourceFS.sftpClient.Open(sourcePath)
	} else {
		sourceFile, err = os.Open(sourcePath)
	}
	if err != nil {
		log.Printf("Error opening source file: %v", err)
		updateProgressBar(progressBar, fmt.Sprintf("Error opening source file: %v", err))
		return
	}
	defer sourceFile.Close()

	// Create target file
	if targetFS.isRemote {
		targetFile, err = targetFS.sftpClient.Create(targetPath)
	} else {
		targetFile, err = os.Create(targetPath)
	}
	if err != nil {
		log.Printf("Error creating target file: %v", err)
		updateProgressBar(progressBar, fmt.Sprintf("Error creating target file: %v", err))
		return
	}
	defer targetFile.Close()

	// Perform file transfer
	updateProgressBar(progressBar, "Transferring file...")

	if sourceFS.isRemote {
		updateProgressBar(progressBar, "Downloading file...")
	} else {
		updateProgressBar(progressBar, "Uploading file...")
	}

	_, err = io.Copy(targetFile, sourceFile)
	if err != nil {
		log.Printf("Error during file transfer: %v", err)
		updateProgressBar(progressBar, fmt.Sprintf("Transfer failed: %v", err))
	} else {
		updateProgressBar(progressBar, "Transfer completed successfully")
	}

	// Update file lists after transfer
	sourceFS.updateList()
	targetFS.updateList()
}

func updateProgressBar(progressBar *tview.TextView, message string) {
	progressBar.Clear()
	progressBar.Write([]byte(message))
}

func INIT_SFTP(host, user, password, port, key string) {
	// SFTP server credentials

	sftpClient, err := createSFTPClient(host+":"+port, user, key, password)
	if err != nil {
		log.Fatalf("Failed to create SFTP client: %v", err)
	}
	defer sftpClient.Close()

	app := tview.NewApplication()
	localFS := NewFileSystem(false, nil)
	remoteFS := NewFileSystem(true, sftpClient)

	flex := tview.NewFlex().
		AddItem(localFS.list, 0, 1, true).
		AddItem(remoteFS.list, 0, 1, false)

	progressBar := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(flex, 0, 1, true).
		AddItem(progressBar, 1, 1, false)

	localFS.updateList()
	remoteFS.updateList()

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			if app.GetFocus() == localFS.list {
				app.SetFocus(remoteFS.list)
			} else {
				app.SetFocus(localFS.list)
			}
			return nil
		case tcell.KeyEnter:
			currentList := app.GetFocus().(*tview.List)
			currentFS := localFS
			targetFS := remoteFS
			if currentList == remoteFS.list {
				currentFS = remoteFS
				targetFS = localFS
			}

			selectedItem := currentList.GetCurrentItem()
			selectedPath := ""

			if selectedItem == 0 {
				// Go to parent directory
				parentDir := filepath.Dir(currentFS.currentPath)
				currentFS.navigateTo(parentDir)
			} else {
				selectedText, _ := currentList.GetItemText(selectedItem)
				if string(selectedText[0]) == "[" {
					leadSpace := strings.Index(selectedText, " ")
					selectedPath = strings.TrimSpace(selectedText[leadSpace:])
				} else {
					selectedPath = selectedText
				}

				if strings.HasSuffix(selectedPath, "/") {
					// Navigate into directory
					newPath := filepath.Join(currentFS.currentPath, strings.TrimSuffix(selectedPath, "/"))
					currentFS.navigateTo(newPath)
				} else {
					// File selected, implement file transfer here
					go transferFile(currentFS, targetFS, selectedPath, progressBar)
				}
			}

			return nil
		}
		return event
	})

	if err := app.SetRoot(mainFlex, true).EnableMouse(true).Run(); err != nil {
		log.Fatalf("Error running application: %v", err)
	}
}
