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
	"log"
	"strings"
)

func encryptAndPushToDB(hostname, column, password string) error {
	if len(password) == 0 {
		return fmt.Errorf("%s is empty", column)
	}

	if len(hostname) == 0 {
		return fmt.Errorf("host is empty")
	}

	encryptedString, err := encrypt([]byte(password))
	if err != nil {
		err = fmt.Errorf("error encrypting %s: %v", column, err)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin the db transaction for %s insertion:%w", column, err)
	}

	stmt, err := tx.Prepare(fmt.Sprintf("INSERT INTO sshprofiles(host,%s) VALUES(?,?) ON CONFLICT(host) DO UPDATE SET %s = excluded.%s;", column, column, column))
	if err != nil {
		return fmt.Errorf("failed to prepare the db for %s insertion:%w", column, err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(hostname, encryptedString)
	if err != nil {
		return fmt.Errorf("failed to insert the %s for the host %v: %w", column, hostname, err)
	}

	log.Printf("%s has been successfully added to the database!\n", column)
	return tx.Commit()
}

func (s *AllConfigs) readAndDecryptFromDB(host, column string, needPass bool) (string, error) {
	var password string

	query := fmt.Sprintf("SELECT %s FROM sshprofiles WHERE host = ? AND %s IS NOT NULL", column, column)

	row := db.QueryRow(query, host)
	err := row.Scan(&password)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("no %s found for host: %s", column, host)
	} else if err != nil {
		return "", fmt.Errorf("read %s query failed: %w", column, err)
	}

	if needPass {
		clearTextPassword, err := decrypt(password)
		if err != nil {
			return `''`, fmt.Errorf("error decrypting %s: %w", column, err)
		}

		return clearTextPassword, nil
	} else {
		return "ok", nil
	}
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
	result := append(nonce, ciphertext...)

	return base64.StdEncoding.EncodeToString(result), nil
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

// decrypt_legacy is used to decrypt the passwords coming from the passwords.json file
// since the encryption mechanism was different and less secure.
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
