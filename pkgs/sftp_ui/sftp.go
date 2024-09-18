package sftp_ui

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
		colorTag = "[red]"
	} else if isDir {
		icon = "📁"
		colorTag = "[blue]"
		suffix = "/"
	} else {
		icon = "📄"
		colorTag = "[green]"
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

const (
	bufferSize = 1024 * 1024 * 4 // 4MB buffer
	numWorkers = 8
)

func transferFile(sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView, app *tview.Application) {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var sourceFile io.ReaderAt
	var targetFile io.WriterAt
	var err error
	var fileSize int64

	// Open source file and get file size
	if sourceFS.isRemote {
		sourceFile, err = sourceFS.sftpClient.Open(sourcePath)
		if err == nil {
			fileInfo, err := sourceFS.sftpClient.Stat(sourcePath)
			if err == nil {
				fileSize = fileInfo.Size()
			}
		}
	} else {
		sourceFile, err = os.Open(sourcePath)
		if err == nil {
			fileInfo, err := os.Stat(sourcePath)
			if err == nil {
				fileSize = fileInfo.Size()
			}
		}
	}
	if err != nil {
		log.Printf("Error opening source file: %v", err)
		updateProgressBar(progressBar, fmt.Sprintf("Error opening source file: %v", err), 0, app, 0)
		return
	}
	defer sourceFile.(io.Closer).Close()

	// Create target file
	if targetFS.isRemote {
		targetFile, err = targetFS.sftpClient.Create(targetPath)
	} else {
		targetFile, err = os.Create(targetPath)
	}
	if err != nil {
		log.Printf("Error creating target file: %v", err)
		updateProgressBar(progressBar, fmt.Sprintf("Error creating target file: %v", err), 0, app, 0)
		return
	}
	defer targetFile.(io.Closer).Close()

	// Perform file transfer with progress
	updateProgressBar(progressBar, "Transferring file", 0, app, 0)

	var totalWritten int64
	spinnerIndex := 0

	// Use a ticker to update the progress bar less frequently
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Use a separate goroutine to update the progress
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				progress := float64(totalWritten) / float64(fileSize) * 100
				updateProgressBar(progressBar, "Transferring", int(progress), app, spinnerIndex)
				spinnerIndex = (spinnerIndex + 1) % 4
			case <-time.After(1 * time.Second):
				// Exit if no update for 1 second
				return
			}
		}
	}()

	// Parallel transfer
	chunkSize := fileSize / int64(numWorkers)
	if chunkSize < bufferSize {
		chunkSize = bufferSize
	}

	var transferWg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		transferWg.Add(1)
		go func(workerID int) {
			defer transferWg.Done()
			buf := make([]byte, bufferSize)
			for offset := int64(workerID) * chunkSize; offset < fileSize; offset += int64(numWorkers) * chunkSize {
				end := offset + chunkSize
				if end > fileSize {
					end = fileSize
				}
				chunk := int64(end - offset)
				for chunk > 0 {
					size := int(chunk)
					if size > bufferSize {
						size = bufferSize
					}
					n, err := sourceFile.ReadAt(buf[:size], offset)
					if err != nil && err != io.EOF {
						log.Printf("Error reading from source: %v", err)
						return
					}
					if n == 0 {
						break
					}
					_, err = targetFile.WriteAt(buf[:n], offset)
					if err != nil {
						log.Printf("Error writing to target: %v", err)
						return
					}
					offset += int64(n)
					chunk -= int64(n)
					atomic.AddInt64(&totalWritten, int64(n))
				}
			}
		}(i)
	}

	transferWg.Wait()
	wg.Wait() // Wait for the progress update goroutine to finish

	updateProgressBar(progressBar, "Transfer completed", 100, app, 0)

	// Update file lists after transfer
	sourceFS.updateList()
	targetFS.updateList()
}

func updateProgressBar(progressBar *tview.TextView, message string, percentage int, app *tview.Application, spinnerIndex int) {
	app.QueueUpdateDraw(func() {
		progressBar.Clear()

		// Get the screen width
		_, _, width, _ := progressBar.GetInnerRect()

		// Calculate the width of the progress bar
		// Subtract 2 for the spinner and colon, and 5 for the percentage display
		barWidth := width - 30

		filled := int(float64(barWidth) * float64(percentage) / 100.0)
		bar := strings.Repeat("[green]#[white]", filled) + strings.Repeat("-", barWidth-filled)

		spinner := []string{"◐", "◓", "◑", "◒"}
		spinnerChar := spinner[spinnerIndex]

		spinners := strings.Repeat(spinnerChar, numWorkers)

		// Format the progress bar with color and spinner
		text := fmt.Sprintf("%s: [green]%s[white][%s] %3d%%",
			message,
			spinners,
			bar,
			percentage)

		progressBar.SetText(text)
	})
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
		SetWrap(false).
		SetTextAlign(tview.AlignLeft)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(flex, 0, 1, true).
		AddItem(progressBar, 2, 1, false) // Keep it at 2 rows for better visibility

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
					go transferFile(currentFS, targetFS, selectedPath, progressBar, app)
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
