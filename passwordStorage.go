package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var keyFile = "encryption.key"
var dataFile = "passwords.json"
var hostPasswords HostPasswords

type HostPasswords []HostPassword

type HostPassword struct {
	Host     string `json:"host"`
	Password string `json:"password"`
}

func loadOrGenerateKey() ([]byte, error) {
	var key []byte
	// Attempt to read the key from the single-row table.
	err := db.QueryRow("SELECT key FROM encryption_key WHERE id = 1").Scan(&key)
	if err == nil {
		return key, nil
	}

	// If no key was found, generate a new one.
	if err == sql.ErrNoRows {

		// Check if the key file exists and read it.
		if keyFile, err = updateFilePath(keyFile); err != nil {
			fmt.Println(err.Error(), "failed to find the absolutepath of the keyfile")
			passAuthSupported = false
		}

		key, err = os.ReadFile(keyFile)
		if err == nil {
			_, err = db.Exec("INSERT INTO encryption_key (id, key) VALUES (1, ?)", key)
			if err != nil {
				return nil, fmt.Errorf("error saving new key to database: %w", err)
			}

			fmt.Println("✅ Data Migration Event: Imported the existing keyfile into the database")
			return key, nil
		}

		// if not keyfile exist, generate a new one
		newKey := make([]byte, 32)
		if _, err := rand.Read(newKey); err != nil {
			return nil, fmt.Errorf("error generating new key: %w", err)
		}

		_, err = db.Exec("INSERT INTO encryption_key (id, key) VALUES (1, ?)", newKey)
		if err != nil {
			return nil, fmt.Errorf("error saving new key to database: %w", err)
		}

		fmt.Println("✅ New encryption key generated and saved to the database.")
		return newKey, nil
	}

	// Handle other potential database errors.
	return nil, fmt.Errorf("database error when retrieving key: %w", err)
}

func encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := aesGCM.Seal(nil, nonce, plaintext, nil)
	result := append(nonce, ciphertext...) // Prepend nonce to ciphertext

	return base64.StdEncoding.EncodeToString(result), nil
}

func decrypt_legacy(ciphertext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	decodedCiphertext, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	if len(decodedCiphertext) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	iv := decodedCiphertext[:aes.BlockSize]
	decodedCiphertext = decodedCiphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(decodedCiphertext, decodedCiphertext)

	return string(decodedCiphertext), nil
}

func decrypt(ciphertext string) (string, error) {
	decodedData, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := aesGCM.NonceSize()
	if len(decodedData) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertextBytes := decodedData[:nonceSize], decodedData[nonceSize:]

	plaintext, err := aesGCM.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		if strings.Contains(err.Error(), "cipher: message authentication failed") {
			pass, err := decrypt_legacy(ciphertext)
			if err != nil {
				return "", fmt.Errorf("failed to decrypt legacy format: %w", err)
			}
			return pass, nil
		}
		return "", err
	}

	return string(plaintext), nil
}

func updateFilePath(fileName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	d := filepath.Join(homeDir, ".ssh")
	absolutepath := filepath.Join(d, fileName)
	return absolutepath, nil
}

func readPassFile() error {

	if _, err := os.Stat(dataFile); err != nil {
		fmt.Printf(" [!] %sIt seems no password database file was created before, so here is one: %v\n%s", green, dataFile, reset)

		if err := createFile(dataFile); err != nil {
			fmt.Printf(" [!] %sfailed to create/access the password database file: %v%s\n", red, dataFile, reset)
			os.Exit(1)
		}
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		return fmt.Errorf("error reading the Password database file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &hostPasswords)
		if err != nil {
			fmt.Printf("error unmarshalling JSON: %v\n", err)
			os.Exit(1)
		}
	}

	return nil
}

func loadCredentials() error {
	rows, err := db.Query("SELECT host, password FROM credentials ORDER BY host")
	if err != nil {
		return fmt.Errorf("failed to query credentials: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hp HostPassword
		if err := rows.Scan(&hp.Host, &hp.Password); err != nil {
			return fmt.Errorf("failed to scan credential row: %w", err)
		}
		hostPasswords = append(hostPasswords, hp)
	}

	// Check for errors that may have occurred during iteration.
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error during credential rows iteration: %w", err)
	}
	if len(hostPasswords) == 0 {
		err := readPassFile()
		if err != nil {
			return fmt.Errorf("error reading password file: %v", err)
		}
		if err := PushPasswordsToDB(); err != nil {
			return fmt.Errorf("error pushing passwords to database: %v", err)
		}
		fmt.Println("✅ Data Migration Event: Filled the password db from the passwords.json file")
	}

	return nil
}

func removeValue(hostname string) error {
	for i, v := range hostPasswords {
		if v.Host == hostname {
			hostPasswords = append(hostPasswords[:i], hostPasswords[i+1:]...)
		}
	}

	stmt, err := db.Prepare("DELETE FROM credentials WHERE host = ?;")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer stmt.Close()

	// Execute the DELETE statement with the provided hostname.
	result, err := stmt.Exec(hostname)
	if err != nil {
		return fmt.Errorf("failed to delete record for host '%s': %w", hostname, err)
	}

	// Check how many rows were affected by the delete operation.
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected after deletion: %w", err)
	}

	if rowsAffected == 0 {
		fmt.Printf("ℹ️ No record found for host '%s'.\n", hostname)
	} else {
		fmt.Printf("🗑️ Successfully deleted %d record(s) for host '%s'.\n", rowsAffected, hostname)
	}

	return nil

}

func updatePasswordDB(profile SSHConfig) {

	p := HostPassword{
		Host:     profile.Host,
		Password: profile.Password,
	}

	db_clone := HostPasswords{}

	for _, entity := range hostPasswords {
		if entity.Host != p.Host {
			db_clone = append(db_clone, entity)
		}
	}

	db_clone = append(db_clone, p)

	hostPasswords = db_clone
}

func PushPasswordsToDB() error {

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare("INSERT INTO credentials(host, password) VALUES(?, ?) ON CONFLICT(host) DO UPDATE SET password = excluded.password;")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}

	defer stmt.Close()

	for _, s := range hostPasswords {
		if _, err := stmt.Exec(s.Host, s.Password); err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert '%s': %w", s.Host, err)
		}
	}

	return tx.Commit()
}
