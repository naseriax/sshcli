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

const (
	numWorkers = 16
)

type AtomicInt64 struct {
	value int64
}

func (a *AtomicInt64) Add(delta int64) int64 {
	return atomic.AddInt64(&a.value, delta)
}

func (a *AtomicInt64) Load() int64 {
	return atomic.LoadInt64(&a.value)
}

func (a *AtomicInt64) Store(val int64) {
	atomic.StoreInt64(&a.value, val)
}

func NewFileSystem(isRemote bool, sftpClient *sftp.Client) *FileSystem {
	fs := &FileSystem{
		currentPath: "/",
		list:        tview.NewList().ShowSecondaryText(false),
		isRemote:    isRemote,
		sftpClient:  sftpClient,
	}

	fs.list.SetSelectedTextColor(tcell.ColorGray)

	fs.list.SetBorder(true).SetTitle(fmt.Sprintf("  %s File System  ", getSystemType(isRemote)))
	return fs
}

func addFileItem(list *tview.List, name string, isDir bool, isSymlink bool) {
	var suffix = ""
	var icon string
	var colorTag string
	if isSymlink {
		icon = "🔗"
		colorTag = "[cyan]"
	} else if isDir {
		icon = "📂"
		colorTag = "[lightcyan]"
		suffix = "/"
	} else {
		icon = "📜"
		colorTag = "[magenta]"
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

// func computeChunkSizes(filesize int64) []int64 {
// 	c := []int64{}
// 	chunkSize := filesize / int64(numWorkers)
// 	var total int64
// 	for i := range numWorkers {
// 		if i < numWorkers-1 {
// 			c = append(c, chunkSize)
// 			total += chunkSize
// 		} else {
// 			c = append(c, filesize-total)
// 		}
// 	}
// 	return c
// }

func transferFile(sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView, app *tview.Application) error {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var sourceFile io.ReaderAt
	var targetFile io.WriterAt
	var err error
	var fileSize int64
	var totalWritten int64

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
		return fmt.Errorf("error opening source file: %v", err)
	}
	defer sourceFile.(io.Closer).Close()

	// Create target file
	if targetFS.isRemote {
		targetFile, err = targetFS.sftpClient.Create(targetPath)

	} else {
		targetFile, err = os.Create(targetPath)
	}
	if err != nil {
		return fmt.Errorf("error creating target file: %v", err)
	}
	defer targetFile.(io.Closer).Close()

	spinIndex := 0

	updateProgress := func() {
		progress := float64(totalWritten) / float64(fileSize) * 100
		updateProgressBar(progressBar, "  Transferring", int(progress), app, spinIndex, filename)
	}

	// Use a ticker to update the progress bar less frequently
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Progress update goroutine
	go func() {
		for range ticker.C {
			spinIndex += 1
			if spinIndex > 9 {
				spinIndex = 0
			}
			updateProgress()
		}
	}()

	var wg sync.WaitGroup
	chunkSize := int(fileSize) / numWorkers
	if fileSize%int64(numWorkers) != 0 {
		chunkSize++ // Adjust the chunk size to ensure the remainder is handled
	}

	errChan := make(chan error, numWorkers) // To capture any errors from goroutines
	progressChan := make(chan int64, numWorkers)
	defer close(errChan)
	defer close(progressChan)
	worker := func(workerIdx int) {
		defer wg.Done()

		// Calculate the offset and size for this worker
		offset := int64(workerIdx) * int64(chunkSize)
		length := chunkSize
		if offset+int64(chunkSize) > fileSize {
			length = int(fileSize - offset) // Adjust for the last chunk if it's smaller
		}

		// Create a buffer to hold the chunk data
		buffer := make([]byte, length)

		// Read from the source
		bytesRead, err := sourceFile.ReadAt(buffer, offset)
		if err != nil && err != io.EOF {
			errChan <- err
			return
		}

		// Write to the target
		bytesWritten, err := targetFile.WriteAt(buffer[:bytesRead], offset)
		if err != nil {
			errChan <- err
			return
		}

		atomic.AddInt64(&totalWritten, int64(bytesWritten))
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i)
	}

	wg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
	}

	updateProgress()

	sourceFS.updateList()
	targetFS.updateList()
	progressBar.SetText(fmt.Sprintf("  Transferred: %s", filename))
	return nil
}

func updateProgressBar(progressBar *tview.TextView, message string, percentage int, app *tview.Application, spinnerIndex int, filename string) {
	app.QueueUpdateDraw(func() {
		progressBar.Clear()

		// Get the screen width
		_, _, width, _ := progressBar.GetInnerRect()

		// Calculate the width of the progress bar
		barWidth := width - (50 + len(filename))

		filled := int(float64(barWidth) * float64(percentage) / 100.0)
		bar := strings.Repeat("[cyan]#[white]", filled) + strings.Repeat("-", barWidth-filled)

		spinner := []string{`⠋`, `⠙`, `⠹`, `⠸`, `⠼`, `⠴`, `⠦`, `⠧`, `⠇`, `⠏`}
		// spinner := []string{`◜`, `◝`, `◞`, `◟`}
		dancingString := ""
		for range spinner {
			dancingString += spinner[spinnerIndex]
			spinnerIndex += 1
			if spinnerIndex >= len(spinner)-1 {
				spinnerIndex = 0
			}
		}

		text := fmt.Sprintf("%s: [lightred]%s[white][%s] %3d%% (%s)",
			message,
			dancingString,
			bar,
			percentage,
			filename)

		progressBar.SetText(text)
	})
}

func INIT_SFTP(host, user, password, port, key string) {
	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Set the log output to the file
	log.SetOutput(file)

	// Optional: log date-time, filename, and line number
	log.SetFlags(log.LstdFlags | log.Lshortfile)

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

	// progressBar.SetBorder(true)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(flex, 0, 1, true).
		AddItem(progressBar, 2, 1, false) // Keep it at 2 rows for better visibility

	localFS.updateList()
	remoteFS.updateList()

	// Initial view (rootfs) has been created. Now it's waiting for the user interaction:
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {

		// On Tab press, switch the focus between local and remote panes.
		case tcell.KeyTab:
			if app.GetFocus() == localFS.list {
				app.SetFocus(remoteFS.list)
			} else {
				app.SetFocus(localFS.list)
			}
			return nil

		// on Enter press, if it's a file, initiate transfer. if it's a folder, go in
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

			//If the Entered items is the first one (..), move 1 level back
			if selectedItem == 0 {
				parentDir := filepath.Dir(currentFS.currentPath)
				currentFS.navigateTo(parentDir)

			} else {
				//Extract the file/folder name without colottags and emojis ==> selectedPath
				selectedText, _ := currentList.GetItemText(selectedItem)
				if string(selectedText[0]) == "[" {
					leadSpace := strings.Index(selectedText, " ")
					selectedPath = strings.TrimSpace(selectedText[leadSpace:])
				} else {
					selectedPath = selectedText
				}

				// if the item has "/" at the end, it's a folder.
				if strings.HasSuffix(selectedPath, "/") {
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
