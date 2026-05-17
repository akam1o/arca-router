package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	// SecureBackupFileMode is used for exported configuration backups.
	SecureBackupFileMode os.FileMode = 0o600
)

// WriteConfigBackupFile writes configuration text to a new secure backup file.
func WriteConfigBackupFile(path, text string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("backup path must not be empty")
	}

	data := []byte(text)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, SecureBackupFileMode)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}

	var writeErr error
	if err := file.Chmod(SecureBackupFileMode); err != nil {
		writeErr = fmt.Errorf("restrict backup file permissions: %w", err)
	} else if _, err := file.Write(data); err != nil {
		writeErr = fmt.Errorf("write backup file: %w", err)
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close backup file: %w", closeErr)
	}
	return nil
}
