package main

import (
	"database/sql"
	"time"
)

const (
	red   = "\033[31m"
	green = "\033[32m"
	reset = "\033[0m"
	// pinkonbrown = "\033[38;2;255;82;197;48;2;155;106;0m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	// cyan        = "\033[36m"
	// orange      = "\033[38;2;255;165;0m"
	BOLD        = "\033[1m"
	consoleIcon = "📟"
	folderIcon  = "🗂️"
	sshIcon     = "🌐"
	hasPassword = "🔑"
	hasProxy    = "📡"
	hasTun      = "🚇"
	hasNote     = "🖍️"
	hasUrl      = "🌐"
	private     = "🙈"
	hasSocks    = "🧦"
	hasphrase   = "🔐"
	goback      = "(b) ⬅️"
)

var (
	CompileTime       = ""
	configPath        = ""
	passAuthSupported = true
	key               []byte
	db                *sql.DB
	legend            string = "🔑: password, 🌐: url, 📡: http proxy, 🚇: ssh tunnel, 🖍️ : note, 🧦:DynamicForward via Socks5, 🔐:  sshkey passphrase"
	isSecure          bool
	msg               = "Legend:\n" + legend + "\n\n"
	port              = "22"
)

type (
	ConsoleConfigs []ConsoleConfig
	ConsoleConfig  struct {
		Host     string
		BaudRate string
		Device   string
		Parity   string
		StopBit  string
		DataBits string
		Folder   string
	}
	AllConfigs []SSHConfig
	SSHConfig  struct {
		Host              string
		HostName          string
		User              string
		Port              string
		Proxy             string
		Sockets           []string
		DynamicSocks      []string
		IdentityFile      string
		Password          string
		sshkey_passphrase string
		Folder            string
		OtherAttribs      []string
	}
	baseModel struct {
		allChoices   []string
		choices      []string
		selected     map[int]string
		cursor       int
		choice       string
		searchQuery  string
		message      string
		inSearchMode bool
		isSSHContext bool
		lastClick    time.Time
	}
	main_model struct {
		baseModel
	}
	ssh_model struct {
		baseModel
	}
)
