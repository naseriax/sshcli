package sftp_ui

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
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

func addFileItem(list *tview.List, name string, t string) {
	var suffix = ""
	var icon string
	var colorTag string

	switch t {
	case "ld":
		icon = "🌀🗂️"
		colorTag = "[lightcyan]"
		suffix = "/"
	case "lf":
		icon = "🌀📝"
		colorTag = "[magenta]"
	case "f":
		icon = "📝"
		colorTag = "[magenta]"
	case "d":
		icon = "🗂️"
		colorTag = "[lightcyan]"
		suffix = "/"
	}

	list.AddItem(fmt.Sprintf("%s%s %s%s", colorTag, icon, name, suffix), "", 0, nil)
}

func getSystemType(isRemote bool) string {
	if isRemote {
		return "Remote"
	}
	return "Local"
}

func (fs *FileSystem) isItFileOrFolder(fPath string, isRemote bool) string {
	var err error
	targetPath := ""
	var targetInfo os.FileInfo
	if isRemote {
		targetPath, err = fs.sftpClient.ReadLink(fPath)
		if err != nil {
			log.Printf("Error reading symlink target: %v\n", err)
			return "l"
		}
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(filepath.Dir(fPath), targetPath)
		}
		targetInfo, err = fs.sftpClient.Stat(targetPath)
		if err != nil {
			log.Printf("Error getting target info: %v\n", err)
			log.Println("The symbolic link may be broken or the target is inaccessible.")
			return "l"
		}

	} else {
		targetPath, err = os.Readlink(fPath)
		if err != nil {
			log.Printf("Error reading symlink target: %v\n", err)
			return "l"
		}
		if !filepath.IsAbs(targetPath) {
			targetPath = filepath.Join(filepath.Dir(fPath), targetPath)
		}
		targetInfo, err = os.Stat(targetPath)
		if err != nil {
			log.Printf("Error getting target info: %v\n", err)
			log.Println("The symbolic link may be broken or the target is inaccessible.")
			return "l"
		}
	}

	if targetInfo.IsDir() {
		return "ld"
	} else {
		return "lf"
	}

}

func SortedFileInfo(l []os.FileInfo) []os.FileInfo {
	sort.Slice(l, func(i, j int) bool {
		return strings.ToLower(l[i].Name()) < strings.ToLower(l[j].Name())
	})
	return l
}

func SortedDirEntry(l []os.DirEntry) []os.DirEntry {
	sort.Slice(l, func(i, j int) bool {
		return strings.ToLower(l[i].Name()) < strings.ToLower(l[j].Name())
	})
	return l
}

func (fs *FileSystem) updateList() {
	t := "d"
	fs.list.Clear()
	fs.list.AddItem("📁 ..", "Go to parent directory", 0, nil)

	if fs.isRemote {
		fileList := make([]os.FileInfo, 0)
		linkDList := make([]os.FileInfo, 0)
		linkFList := make([]os.FileInfo, 0)
		folderList := make([]os.FileInfo, 0)
		files, err := fs.sftpClient.ReadDir(fs.currentPath)
		if err != nil {
			log.Printf("Error reading remote directory: %v", err)
			return
		}
		for _, file := range files {

			fPath := filepath.Join(fs.currentPath, file.Name())
			if file.Mode()&os.ModeSymlink != 0 {
				t = fs.isItFileOrFolder(fPath, true)
			} else {
				if file.IsDir() {
					t = "d"
				} else {
					t = "f"
				}
			}

			switch t {
			case "f":
				fileList = append(fileList, file)
			case "d":
				folderList = append(folderList, file)
			case "ld":
				linkDList = append(linkDList, file)
			case "lf":
				linkFList = append(linkFList, file)
			}
		}

		for _, f := range SortedFileInfo(linkFList) {
			addFileItem(fs.list, f.Name(), "lf")
		}
		for _, f := range SortedFileInfo(linkDList) {
			addFileItem(fs.list, f.Name(), "ld")
		}
		for _, f := range SortedFileInfo(folderList) {
			addFileItem(fs.list, f.Name(), "d")
		}
		for _, f := range SortedFileInfo(fileList) {
			addFileItem(fs.list, f.Name(), "f")
		}

	} else {
		fileList := make([]os.DirEntry, 0)
		linkDList := make([]os.DirEntry, 0)
		linkFList := make([]os.DirEntry, 0)
		folderList := make([]os.DirEntry, 0)
		entries, err := os.ReadDir(fs.currentPath)
		if err != nil {
			log.Printf("Error reading local directory: %v", err)
			return
		}

		for _, entry := range entries {

			fPath := filepath.Join(fs.currentPath, entry.Name())

			info, err := entry.Info()
			if err != nil {
				log.Printf("Error getting file info: %v", err)
				continue
			}

			if info.Mode()&os.ModeSymlink != 0 {
				t = fs.isItFileOrFolder(fPath, false)

			} else {
				if entry.IsDir() {
					t = "d"
				} else {
					t = "f"
				}
			}

			switch t {
			case "f":
				fileList = append(fileList, entry)
			case "d":
				folderList = append(folderList, entry)
			case "ld":
				linkDList = append(linkDList, entry)
			case "lf":
				linkFList = append(linkFList, entry)
			}
		}

		for _, f := range SortedDirEntry(linkFList) {
			addFileItem(fs.list, f.Name(), "lf")
		}
		for _, f := range SortedDirEntry(linkDList) {
			addFileItem(fs.list, f.Name(), "ld")
		}
		for _, f := range SortedDirEntry(folderList) {
			addFileItem(fs.list, f.Name(), "d")
		}
		for _, f := range SortedDirEntry(fileList) {
			addFileItem(fs.list, f.Name(), "f")
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
				progressData := progressSplit[len(progressSplit)-3:]
				progressChan <- strings.Join(progressData, "^")
				if strings.Contains(lineBuffer, "100%") {
					time.Sleep(1 * time.Second)
					break
				}
				lineBuffer = ""
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
	_, err = ptmx.Write([]byte(fmt.Sprintf("%s '%s' '%s'\n", direction, localFile, remoteFile)))
	if err != nil {
		return fmt.Errorf("failed to send %s command: %v", direction, err)

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

func transferFile(hostId string, sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView, app *tview.Application, flex_pbars *tview.Flex) error {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var (
		currentProgress string
		spinIndex       = 0
	)

	updateProgress := func() {
		updateProgressBar(progressBar, "  Transferring", currentProgress, app, spinIndex, filename)
	}

	ticker := time.NewTicker(100 * time.Millisecond)

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
	sourcePath = filepath.Clean(sourcePath)
	targetPath = filepath.Clean(targetPath)

	err := sftpTransfer(hostId, sourcePath, targetPath, direction, &currentProgress)
	if err != nil {
		log.Println("upload err:", err)
		return err
	}

	ticker.Stop()

	app.QueueUpdateDraw(func() {
		progressBar.Clear()
		progressBar.SetText(fmt.Sprintf("  Transferred: [%s]", filename))
	})

	sourceFS.updateList()
	targetFS.updateList()

	flex_pbars.RemoveItem(progressBar)

	return nil
}

func updateProgressBar(progressBar *tview.TextView, message, currentProgress string, app *tview.Application, spinnerIndex int, filename string) {
	app.QueueUpdateDraw(func() {
		progressBar.Clear()

		_, _, width, _ := progressBar.GetInnerRect()

		barWidth := width - (40 + len(filename))
		progressSplit := strings.Split(currentProgress, "^")

		if len(progressSplit) < 3 {
			return
		}

		num, found := strings.CutSuffix(progressSplit[0], `%`)
		if !found {
			return
		}

		var filled int
		num_int, err := strconv.Atoi(num)
		if err != nil {
			filled = 1
		} else {
			filled = int(float64(barWidth) * float64(num_int) / 100.0)
		}

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

func detectItemType(item string) string {

	if strings.Contains(item, "🌀🗂️") {
		return "ld"
	}

	if strings.Contains(item, "🌀📝") {
		return "lf"
	}

	if strings.Contains(item, "📝") {
		return "f"
	}

	return "d"

}

func INIT_SFTP(hostId, host, user, password, port, key string) error {

	file, err := os.OpenFile("sftp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	log.SetOutput(file)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	sftpClient, sshClient, err := opentheGates(host+":"+port, user, key, password)
	if err != nil {
		log.Printf("Failed to create SFTP client: %v\n", err)
		return err
	}

	defer sshClient.Close()
	defer sftpClient.Close()

	app := tview.NewApplication()
	localFS := NewFileSystem(false, nil, nil)
	remoteFS := NewFileSystem(true, sftpClient, sshClient)

	flex := tview.NewFlex().
		AddItem(localFS.list, 0, 1, true).
		AddItem(remoteFS.list, 0, 1, false)

	flex_pbar := tview.NewFlex()
	flex_pbar.SetDirection(tview.FlexRow)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(flex, 0, 5, true).
		AddItem(flex_pbar, 0, 1, false)

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
				itemType := detectItemType(selectedText)
				if string(selectedText[0]) == "[" {
					leadSpace := strings.Index(selectedText, " ")
					selectedPath = strings.TrimSpace(selectedText[leadSpace:])
				} else {
					selectedPath = selectedText
				}

				// if the item has "/" at the end, it's a folder.
				if strings.Contains(itemType, "d") {
					newPath := filepath.Join(currentFS.currentPath, strings.TrimSuffix(selectedPath, "/"))
					currentFS.navigateTo(newPath)
				} else if itemType == "f" {
					// File selected, implement file transfer here
					p := tview.NewTextView().
						SetDynamicColors(true).
						SetRegions(true).
						SetWrap(false).
						SetTextAlign(tview.AlignLeft)

					flex_pbar.AddItem(p, 0, 1, false)

					go transferFile(hostId, currentFS, targetFS, selectedPath, p, app, flex_pbar)
				}
			}

			return nil
		}
		return event
	})

	if err := app.SetRoot(mainFlex, true).EnableMouse(true).Run(); err != nil {
		log.Printf("Error running application: %v", err)
		return err
	}

	return nil
}
