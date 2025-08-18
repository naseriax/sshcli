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
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gdamore/tcell/v2"
	"github.com/pkg/sftp"
	"github.com/rivo/tview"
	"golang.org/x/crypto/ssh"
)

type FileSystem struct {
	currentPath   string
	list          *tview.List
	isRemote      bool
	sftpClient    *sftp.Client
	selectedItems map[int]bool
	mu            sync.Mutex
}

// type TransferJob struct {
// 	filename    string
// 	sourcePath  string
// 	targetPath  string
// 	progressBar *tview.TextView
// }

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
		currentPath:   "/",
		list:          tview.NewList().ShowSecondaryText(false),
		isRemote:      isRemote,
		sftpClient:    sftpClient,
		selectedItems: make(map[int]bool),
	}

	fs.list.SetSelectedTextColor(tcell.ColorGray)
	fs.list.SetBorder(true).SetTitle(fmt.Sprintf("  %s File System  ", getSystemType(isRemote)))
	return fs
}

func addFileItem(list *tview.List, name string, t string, isSelected bool) {
	var suffix = ""
	var icon string
	var colorTag string
	var selectionMark string = "  "

	if isSelected {
		selectionMark = "✓ "
	}

	switch t {
	case "ld":
		icon = "🌀📁"
		colorTag = "[lightcyan]"
		suffix = "/"
	case "lf":
		icon = "🌀📄"
		colorTag = "[magenta]"
	case "f":
		icon = "📄"
		colorTag = "[magenta]"
	case "d":
		icon = "📁"
		colorTag = "[lightcyan]"
		suffix = "/"
	}

	list.AddItem(fmt.Sprintf("%s%s%s %s%s", selectionMark, colorTag, icon, name, suffix), "", 0, nil)
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

func loadingBar(StopChan chan bool) {
	spinner := []string{`▒▒▒▒`, `▒▒▒▒`, `▓▒▒▒`, `█▓▒▒`, `██▓▒`, `███▓`, `████`}
	for {
		select {
		case <-StopChan:
			return
		default:
			for _, c := range spinner {
				fmt.Print("\033[H")
				fmt.Print(c)
				time.Sleep(80 * time.Millisecond)
			}
		}
	}
}

func (fs *FileSystem) updateList() {
	StopChan := make(chan bool)
	go loadingBar(StopChan)
	defer func() { StopChan <- true }()

	t := "d"
	fs.list.Clear()
	fs.list.AddItem("📁 ..", "Go to parent directory", 0, nil)

	// Track files with their indices
	itemIndex := 1 // Start from 1 since 0 is ".."

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
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "lf", isSelected)
			itemIndex++
		}
		for _, f := range SortedFileInfo(linkDList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "ld", isSelected)
			itemIndex++
		}
		for _, f := range SortedFileInfo(folderList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "d", isSelected)
			itemIndex++
		}
		for _, f := range SortedFileInfo(fileList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "f", isSelected)
			itemIndex++
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
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "lf", isSelected)
			itemIndex++
		}
		for _, f := range SortedDirEntry(linkDList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "ld", isSelected)
			itemIndex++
		}
		for _, f := range SortedDirEntry(folderList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "d", isSelected)
			itemIndex++
		}
		for _, f := range SortedDirEntry(fileList) {
			isSelected := fs.selectedItems[itemIndex]
			addFileItem(fs.list, f.Name(), "f", isSelected)
			itemIndex++
		}
	}
}

func (fs *FileSystem) navigateTo(path string) {
	fs.currentPath = path
	fs.clearSelection()
	fs.updateList()
}

func (fs *FileSystem) toggleSelection(index int) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.selectedItems[index] {
		delete(fs.selectedItems, index)
	} else {
		fs.selectedItems[index] = true
	}
}

func (fs *FileSystem) clearSelection() {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.selectedItems = make(map[int]bool)
}

func (fs *FileSystem) getSelectedFiles() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var files []string
	for idx := range fs.selectedItems {
		if idx > 0 { // Skip ".." entry
			text, _ := fs.list.GetItemText(idx)
			filename := extractFilename(text)
			if filename != "" {
				files = append(files, filename)
			}
		}
	}
	return files
}

func extractFilename(text string) string {
	// Remove selection mark, color tags, and icons
	text = strings.TrimPrefix(text, "✓ ")
	text = strings.TrimPrefix(text, "  ")

	// Find the position after icons
	startIdx := 0
	for i, r := range text {
		if r == ' ' && i > 0 {
			startIdx = i + 1
			break
		}
	}

	if startIdx == 0 {
		return ""
	}

	filename := text[startIdx:]
	filename = strings.TrimSuffix(filename, "/")

	// Remove color tags if present
	if strings.Contains(filename, "[") {
		parts := strings.Split(filename, " ")
		if len(parts) > 1 {
			filename = strings.Join(parts[1:], " ")
		}
	}

	return filename
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

	if key != "" && key != " " {
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

func transferFile(hostId string, sourceFS, targetFS *FileSystem, filename string, progressBar *tview.TextView, app *tview.Application, flex_pbars *tview.Flex, jobNum, totalJobs int) error {
	sourcePath := filepath.Join(sourceFS.currentPath, filename)
	targetPath := filepath.Join(targetFS.currentPath, filename)

	var (
		currentProgress string
		spinIndex       = 0
	)

	updateProgress := func() {
		updateProgressBar(progressBar, fmt.Sprintf("  [%d/%d] Transferring", jobNum, totalJobs), currentProgress, app, spinIndex, filename)
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
		progressBar.SetText(fmt.Sprintf("  ✅ [%d/%d] Completed: [%s]", jobNum, totalJobs, filename))
		progressBar.SetTextColor(tcell.ColorGreen)
	})

	sourceFS.updateList()
	targetFS.updateList()

	// Keep completed progress bars visible for a while before removing
	go func() {
		time.Sleep(5 * time.Second)
		app.QueueUpdateDraw(func() {
			flex_pbars.RemoveItem(progressBar)
		})
	}()

	return nil
}

func updateProgressBar(progressBar *tview.TextView, message, currentProgress string, app *tview.Application, spinnerIndex int, filename string) {
	app.QueueUpdateDraw(func() {
		progressBar.Clear()

		_, _, width, _ := progressBar.GetInnerRect()

		barWidth := width - (45 + len(filename))
		if barWidth < 10 {
			barWidth = 10
		}

		progressSplit := strings.Split(currentProgress, "^")

		if len(progressSplit) < 3 {
			progressBar.SetText(fmt.Sprintf("%s: (%s) [yellow]⏳ Waiting...", message, filename))
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

		bar := strings.Repeat("[cyan]#[white]", filled) + strings.Repeat("░", barWidth-filled)

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
	if strings.Contains(item, "🌀📁") {
		return "ld"
	}
	if strings.Contains(item, "🌀📄") {
		return "lf"
	}
	if strings.Contains(item, "📄") {
		return "f"
	}
	return "d"
}

func createLegend() *tview.TextView {
	legend := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false).
		SetTextAlign(tview.AlignCenter)

	legendText := `[yellow]【 Keyboard Shortcuts 】[white][cyan]Tab[white]: Switch panes │ [cyan]Space[white]: Select/Deselect │ [cyan]Enter[white]: Open/Transfer │ [cyan]a[white]: Select All │ [cyan]d[white]: Deselect All │ [cyan]t[white]: Transfer Selected │ [cyan]q/Ctrl+C[white]: Quit`
	legend.SetText(legendText)
	legend.SetBorder(true).SetBorderPadding(0, 0, 1, 1)
	return legend
}

func createStatusBar(localFS, remoteFS *FileSystem) *tview.TextView {
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false)

	updateStatus := func() {
		localSelected := len(localFS.getSelectedFiles())
		remoteSelected := len(remoteFS.getSelectedFiles())

		statusText := fmt.Sprintf(" [green]Local:[white] %s [yellow](%d selected)[white] │ [blue]Remote:[white] %s [yellow](%d selected)[white] ",
			localFS.currentPath, localSelected,
			remoteFS.currentPath, remoteSelected)

		status.SetText(statusText)
	}

	updateStatus()
	return status
}

func INIT_SFTP(hostId, host, user, password, port, key string) error {
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

	// Create progress bar container with title
	flex_pbar := tview.NewFlex()
	flex_pbar.SetDirection(tview.FlexRow)
	flex_pbar.SetBorder(true).SetTitle(" Transfer Queue ")

	// Create legend
	legend := createLegend()

	// Create status bar
	statusBar := createStatusBar(localFS, remoteFS)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(legend, 3, 0, false).
		AddItem(flex, 0, 5, true).
		AddItem(flex_pbar, 8, 0, false).
		AddItem(statusBar, 1, 0, false)

	localFS.updateList()
	remoteFS.updateList()

	go closeAll(prepareOsSig(), app)

	// Function to update status bar
	updateStatusBar := func() {
		localSelected := len(localFS.getSelectedFiles())
		remoteSelected := len(remoteFS.getSelectedFiles())

		statusText := fmt.Sprintf(" [green]Local:[white] %s [yellow](%d selected)[white] │ [blue]Remote:[white] %s [yellow](%d selected)[white] ",
			localFS.currentPath, localSelected,
			remoteFS.currentPath, remoteSelected)

		statusBar.SetText(statusText)
	}

	// Function to transfer selected files
	transferSelectedFiles := func(sourceFS, targetFS *FileSystem) {
		selectedFiles := sourceFS.getSelectedFiles()
		if len(selectedFiles) == 0 {
			return
		}

		totalJobs := len(selectedFiles)
		for i, filename := range selectedFiles {
			p := tview.NewTextView().
				SetDynamicColors(true).
				SetRegions(true).
				SetWrap(false).
				SetTextAlign(tview.AlignLeft)

			flex_pbar.AddItem(p, 1, 0, false)

			jobNum := i + 1
			go transferFile(hostId, sourceFS, targetFS, filename, p, app, flex_pbar, jobNum, totalJobs)
		}

		sourceFS.clearSelection()
		updateStatusBar()
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		currentList := app.GetFocus().(*tview.List)
		currentFS := localFS
		targetFS := remoteFS

		if currentList == remoteFS.list {
			currentFS = remoteFS
			targetFS = localFS
		}

		switch event.Key() {
		case tcell.KeyTab:
			if app.GetFocus() == localFS.list {
				app.SetFocus(remoteFS.list)
			} else {
				app.SetFocus(localFS.list)
			}
			return nil

		case tcell.KeyRune:
			switch event.Rune() {
			case ' ': // Space for selection
				selectedItem := currentList.GetCurrentItem()
				if selectedItem > 0 { // Don't select ".."
					currentFS.toggleSelection(selectedItem)
					currentFS.updateList()
					updateStatusBar()
				}
				return nil

			case 'a', 'A': // Select all
				for i := 1; i < currentList.GetItemCount(); i++ {
					text, _ := currentList.GetItemText(i)
					itemType := detectItemType(text)
					if itemType == "f" || itemType == "lf" { // Only select files
						currentFS.selectedItems[i] = true
					}
				}
				currentFS.updateList()
				updateStatusBar()
				return nil

			case 'd', 'D': // Deselect all
				currentFS.clearSelection()
				currentFS.updateList()
				updateStatusBar()
				return nil

			case 't', 'T': // Transfer selected files
				transferSelectedFiles(currentFS, targetFS)
				return nil

			case 'q', 'Q': // Quit
				app.Stop()
				return nil
			}

		case tcell.KeyEnter:
			selectedItem := currentList.GetCurrentItem()

			// Check if there are selected items
			if len(currentFS.getSelectedFiles()) > 0 {
				// Transfer all selected files
				transferSelectedFiles(currentFS, targetFS)
			} else {
				// Original behavior for single item
				selectedPath := ""

				if selectedItem == 0 {
					parentDir := filepath.Dir(currentFS.currentPath)
					currentFS.navigateTo(parentDir)
					updateStatusBar()
				} else {
					selectedText, _ := currentList.GetItemText(selectedItem)
					itemType := detectItemType(selectedText)
					selectedPath = extractFilename(selectedText)

					if strings.Contains(itemType, "d") {
						newPath := filepath.Join(currentFS.currentPath, selectedPath)
						currentFS.navigateTo(newPath)
						updateStatusBar()
					} else if itemType == "f" || itemType == "lf" {
						// Single file transfer
						p := tview.NewTextView().
							SetDynamicColors(true).
							SetRegions(true).
							SetWrap(false).
							SetTextAlign(tview.AlignLeft)

						flex_pbar.AddItem(p, 1, 0, false)
						go transferFile(hostId, currentFS, targetFS, selectedPath, p, app, flex_pbar, 1, 1)
					}
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
