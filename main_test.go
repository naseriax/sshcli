package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUpdateSSHConfig tests the updateSSHConfig function with various scenarios
func TestUpdateSSHConfig(t *testing.T) {
	// Create a temporary directory for test SSH config files
	tempDir, err := os.MkdirTemp("", "ssh_config_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Helper function to create a test config file
	createTestConfig := func(content string) string {
		configPath := filepath.Join(tempDir, "config")
		err := os.WriteFile(configPath, []byte(content), 0600)
		if err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}
		return configPath
	}

	// Helper function to read config file content
	readConfig := func(path string) string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}
		return string(content)
	}

	// Test cases
	tests := []struct {
		name           string
		initialConfig  string
		inputConfig    SSHConfig
		expectedConfig string
	}{
		// ... (test cases remain the same)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTestConfig(tt.initialConfig)

			// Mock the os.UserHomeDir function
			oldUserHomeDir := osUserHomeDir
			osUserHomeDir = func() (string, error) {
				return tempDir, nil
			}
			defer func() { osUserHomeDir = oldUserHomeDir }()

			err := updateSSHConfig(configPath, tt.inputConfig)
			if err != nil {
				t.Fatalf("updateSSHConfig failed: %v", err)
			}

			actualConfig := readConfig(configPath)
			if actualConfig != tt.expectedConfig {
				t.Errorf("Config mismatch.\nExpected:\n%s\nActual:\n%s", tt.expectedConfig, actualConfig)
			}
		})
	}
}

// TestParseField tests the parseField function with different input strings
func TestParseField(t *testing.T) {
	// ... (test cases and implementation remain the same)
}

// TestGetUpdatedValue tests the getUpdatedValue function with different fields
func TestGetUpdatedValue(t *testing.T) {
	// ... (test cases and implementation remain the same)
}

// TestAddMissingFields tests the addMissingFields function with different scenarios of updated fields
func TestAddMissingFields(t *testing.T) {
	// ... (test cases and implementation remain the same)
}

// TestDeleteSSHProfile tests the deleteSSHProfile function with various scenarios
func TestDeleteSSHProfile(t *testing.T) {
	// Create a temporary directory for test SSH config files
	tempDir, err := os.MkdirTemp("", "ssh_config_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Helper function to create a test config file
	createTestConfig := func(content string) string {
		configPath := filepath.Join(tempDir, "config")
		err := os.WriteFile(configPath, []byte(content), 0600)
		if err != nil {
			t.Fatalf("Failed to write test config: %v", err)
		}
		return configPath
	}

	// Helper function to read config file content
	readConfig := func(path string) string {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to read config file: %v", err)
		}
		return string(content)
	}

	// Test cases
	tests := []struct {
		name           string
		initialConfig  string
		hostToDelete   string
		expectedConfig string
		expectError    bool
	}{
		// ... (test cases remain the same)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := createTestConfig(tt.initialConfig)

			// Mock the os.UserHomeDir function
			oldUserHomeDir := osUserHomeDir
			osUserHomeDir = func() (string, error) {
				return tempDir, nil
			}
			defer func() { osUserHomeDir = oldUserHomeDir }()

			err := deleteSSHProfile(tt.hostToDelete)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected an error, but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("deleteSSHProfile failed: %v", err)
				}

				actualConfig := readConfig(configPath)
				if actualConfig != tt.expectedConfig {
					t.Errorf("Config mismatch.\nExpected:\n%s\nActual:\n%s", tt.expectedConfig, actualConfig)
				}
			}
		})
	}
}

// Helper function to compare string slices
// func stringSlicesEqual(a, b []string) bool {
// 	if len(a) != len(b) {
// 		return false
// 	}
// 	for i := range a {
// 		if a[i] != b[i] {
// 			return false
// 		}
// 	}
// 	return true
// }

// Mock for os.UserHomeDir
var osUserHomeDir = os.UserHomeDir
