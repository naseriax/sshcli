package sftp_ui

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
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

func closeAll(c chan os.Signal, app *tview.Application) {

	<-c
	log.Println("\nctrl-c detected!")

	time.Sleep(3 * time.Second)
	app.Stop()
}

func prepareOsSig() chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	return c
}

func NewFileSystem(isRemote bool, sftpClient *sftp.Client, sshClient *ssh.Client) *FileSystem {
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

func opentheGates(host, user, key, password string) (*sftp.Client, *ssh.Client, error) {
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
		return nil, nil, fmt.Errorf("failed to dial: %v", err)
	}

	client, err := sftp.NewClient(conn, sftp.MaxPacket(32768))
	if err != nil {
		log.Println(err)
		return nil, conn, fmt.Errorf("failed to create SFTP client: %v", err)
	}

	return client, conn, nil
}

func sftpTransfer(remote, localFile, remoteFile, direction string, currentProgress *string) error {
	cmd := exec.Command("sftp", remote)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start sftp with pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()
	done := make(chan error, 1)
	progressChan := make(chan string)
	go func() {
		reader := bufio.NewReader(ptmx)
		var lineBuffer string
		for {
			char, err := reader.ReadByte()
			if err != nil {
				break
			}
			lineBuffer += string(char)
			if strings.Contains(lineBuffer, "B/s") {
				progressSplit := strings.Fields(lineBuffer)
				progressChan <- progressSplit[3] + " " + progressSplit[4] + " " + progressSplit[5]
				lineBuffer = ""
				if progressSplit[3] == "100%" {
					time.Sleep(1 * time.Second)
					break
				}
			}
		}
		close(progressChan)
		done <- nil
	}()

	go func() {
		var lastProgress string
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case progress, ok := <-progressChan:
				if !ok {
					return
				}
				lastProgress = progress
			case <-ticker.C:
				if lastProgress != "" {
					if strings.Contains(lastProgress, "B/s") {
						*currentProgress = lastProgress
					}

					if strings.Contains(lastProgress, "100%") {
						ticker.Stop()
					}
				}
			}
		}
	}()

	_, err = ptmx.Write([]byte(fmt.Sprintf("%s %s %s\n", direction, localFile, remoteFile)))
	if err != nil {
		return fmt.Errorf("failed to send put command: %v", err)
	}
	<-done
	_, err = ptmx.Write([]byte("exit\n"))
	if err != nil {
		return fmt.Errorf("failed to send exit command: %v", err)
	}
	err = cmd.Wait()
	if err != nil {
		return fmt.Errorf("sftp exited with error: %v", err)
	}
	return nil
}

func transferFile(hostId string, sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView, app *tview.Application) error {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var (
		currentProgress string
		t               time.Duration
		spinIndex       = 0
	)

	updateProgress := func() {
		updateProgressBar(progressBar, "  Transferring", currentProgress, app, spinIndex, filename)
	}

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			spinIndex += 1
			if spinIndex > 9 {
				spinIndex = 0
			}
			updateProgress()
		}
	}()

	direction := "put"
	if sourceFS.isRemote {
		direction = "get"
	}

	err := sftpTransfer(hostId, sourcePath, targetPath, direction, &currentProgress)
	if err != nil {
		log.Println("upload err:", err)
		return err
	}

	updateProgress()

	app.QueueUpdateDraw(func() {
		progressBar.Clear()
		progressBar.SetText(fmt.Sprintf("  Transferred: [%s] in %s", filename, t))
	})

	sourceFS.updateList()
	targetFS.updateList()
	return nil
}

func updateProgressBar(progressBar *tview.TextView, message, currentProgress string, app *tview.Application, spinnerIndex int, filename string) {
	app.QueueUpdateDraw(func() {
		progressBar.Clear()

		_, _, width, _ := progressBar.GetInnerRect()

		barWidth := width - (40 + len(filename))
		progressSplit := strings.Fields(currentProgress)

		if len(progressSplit) < 3 {
			log.Println(currentProgress)
			return
		}

		num, found := strings.CutSuffix(progressSplit[0], `%`)
		if !found {
			return
		}

		num_int, _ := strconv.Atoi(num)
		filled := int(float64(barWidth) * float64(num_int) / 100.0)
		bar := strings.Repeat("[cyan]#[white]", filled) + strings.Repeat("-", barWidth-filled)

		spinner := []string{`⠋`, `⠙`, `⠹`, `⠸`, `⠼`, `⠴`, `⠦`, `⠧`, `⠇`, `⠏`}

		dancingString := spinner[spinnerIndex]

		text := fmt.Sprintf("%s: (%s) [lightred]%s [%s] %3d%% %s",
			message,
			filename,
			dancingString,
			bar,
			num_int,
			progressSplit[2],
		)

		progressBar.SetText(text)
	})
}

func INIT_SFTP(hostId, host, user, password, port, key string) {

	file, err := os.OpenFile("sftp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	sftpClient, sshClient, err := opentheGates(host+":"+port, user, key, password)
	if err != nil {
		log.Fatalf("Failed to create SFTP client: %v", err)
	}

	defer sshClient.Close()
	defer sftpClient.Close()

	app := tview.NewApplication()
	localFS := NewFileSystem(false, nil, nil)
	remoteFS := NewFileSystem(true, sftpClient, sshClient)

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

	go closeAll(prepareOsSig(), app)

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
					go transferFile(hostId, currentFS, targetFS, selectedPath, progressBar, app)
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
