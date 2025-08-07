package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func encryptAndPushPassToDB(hostname, password string) error {
	if len(password) == 0 {
		return fmt.Errorf("password is empty")
	}

	if len(hostname) == 0 {
		return fmt.Errorf("host is empty")
	}

	encryptedString, err := encrypt([]byte(password))
	if err != nil {
		err = fmt.Errorf("error encrypting password: %v", err)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for password insertion:%v", err)
	}

	stmt, err := tx.Prepare("INSERT INTO sshprofiles(host,password) VALUES(?,?) ON CONFLICT(host) DO UPDATE SET password = excluded.password;")
	if err != nil {
		return fmt.Errorf("failed to prepare the db for password insertion:%v", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(hostname, encryptedString)
	if err != nil {
		return fmt.Errorf("failed to insert the password for the host %v: %v", hostname, err)
	}

	fmt.Println("\nPassword has been successfully added to the password database!")
	return tx.Commit()
}

func readAndDecryptPassFromDB(host string) (string, error) {
	var password string

	query := "SELECT password FROM sshprofiles WHERE host = ? AND password IS NOT NULL"

	row := db.QueryRow(query, host)
	err := row.Scan(&password)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no password found for host: %s", host)
	} else if err != nil {
		return "", fmt.Errorf("read password query failed: %w", err)
	}

	clearTextPassword, err := decrypt(password)
	if err != nil {
		return `''`, fmt.Errorf("error decrypting password: %v", err)
	}

	return clearTextPassword, nil
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
