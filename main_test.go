package main

import (
	"testing"
)

func TestFormatField(t *testing.T) {
	fieldName := "TestField"
	fieldValue := "TestValue"
	expected := "    TestField TestValue\n"
	result := formatField(fieldName, fieldValue)

	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestFormatProfile(t *testing.T) {
	profile := SSHConfigProfile{
		Host:         "testHost",
		HostName:     "testHostName",
		User:         "testUser",
		Port:         "22",
		IdentityFile: "~/.ssh/id_rsa",
	}

	expected := "Host testHost\n    HostName testHostName\n    User testUser\n    Port 22\n    IdentityFile ~/.ssh/id_rsa\n"
	result := formatProfile(profile)

	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}
