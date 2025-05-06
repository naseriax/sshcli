package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var keyFile = "encryption.key"
var dataFile = "passwords.json"
var hostPasswords HostPasswords

type HostPasswords []HostPassword

type HostPassword struct {
	Host        string `json:"host"`
	Password    string `json:"password"`
	IsEncrypted bool   `json:"isEncrypted"`
}

func loadOrGenerateKey() ([]byte, error) {
	var err error
	if keyFile, err = updateFilePath(keyFile); err != nil {
		fmt.Println(err.Error(), "failed to find the absolutepath of the keyfile")
		passAuthSupported = false
	}

	key, err = os.ReadFile(keyFile)
	if err == nil {
		return key, nil
	}

	// Generate a new key if the file doesn't exist
	key = make([]byte, 32) // AES-256 key
	_, err = rand.Read(key)
	if err != nil {
		return key, fmt.Errorf("error generating key: %v", err)
	}

	err = os.WriteFile(keyFile, key, 0600)
	if err != nil {
		return key, fmt.Errorf("error writing key file: %v", err)
	}
	fmt.Println("A new key has been generated")

	return key, nil
}

func encrypt(plaintext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(ciphertext string, key []byte) (string, error) {
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

func updateFilePath(fileName string) (string, error) {
	// execPath, err := os.Executable()

	// if err != nil {
	// 	return "", fmt.Errorf("error getting executable path:%v", err)
	// }
	// execDir := filepath.Dir(execPath)
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	d := filepath.Join(homeDir, ".ssh")
	absolutepath := filepath.Join(d, fileName)
	return absolutepath, nil
}

func readPassFile() (HostPasswords, error) {

	if _, err := os.Stat(dataFile); err != nil {
		fmt.Printf("It seems no password database file was created before, so here is one: %v. Trying to create it...\n", dataFile)

		if err := createFile(dataFile); err != nil {
			log.Fatalf("failed to create/access the password database file: %v", dataFile)
		}
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		return hostPasswords, fmt.Errorf("error reading the Password database file: %v", err)
	}

	if len(data) != 0 {
		err = json.Unmarshal(data, &hostPasswords)
		if err != nil {
			log.Fatalf("error unmarshalling JSON: %v", err)
			return hostPasswords, fmt.Errorf("error unmarshalling JSON: %v", err)
		}
	}

	return hostPasswords, nil
}

func removeValue(s HostPasswords, host string) HostPasswords {
	for i, v := range s {
		if v.Host == host {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func updatePasswordDB(profile SSHConfig) {

	p := HostPassword{
		Host:        profile.Host,
		Password:    profile.Password,
		IsEncrypted: false,
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

func EncryptOrDecryptPassword(host string, key []byte, mode string) (string, error) {

	var (
		updatedString string
		err           error
	)

	original_host := host

	for i, v := range hostPasswords {
		if len(host) == 0 {
			host = v.Host
		}
		if v.Host == host {
			if strings.EqualFold(mode, "enc") {
				if !v.IsEncrypted {
					updatedString, err = encrypt([]byte(v.Password), key)
					if err != nil {
						err = fmt.Errorf("error encrypting password: %v", err)
						return `''`, err
					}
					if len(original_host) == 0 {
						hostPasswords[i].IsEncrypted = true
						hostPasswords[i].Password = updatedString
					}
				} else {
					updatedString = v.Password
				}
			} else {
				if v.IsEncrypted {
					updatedString, err = decrypt(v.Password, key)
					if err != nil {
						err = fmt.Errorf("error decrypting password: %v", err)
						return `''`, err
					}

				} else {
					updatedString = v.Password
				}
			}
		}

		host = original_host
	}
	return updatedString, nil
}

func writeUpdatedPassDbToFile() error {

	// make sure all records are encrypted:
	_, err := EncryptOrDecryptPassword("", key, "enc")
	if err != nil {
		log.Println("failed to encrypt the password datababe. Decrypted data may be written to the passwords.json file")
	}

	encryptedData, err := json.MarshalIndent(hostPasswords, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshalling JSON: %v", err)
	}

	err = os.WriteFile(dataFile, encryptedData, 0644)
	if err != nil {
		return fmt.Errorf("error writing file: %v", err)
	}

	return nil
}
