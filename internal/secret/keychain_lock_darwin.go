//go:build darwin

package secret

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func withKeychainLock(fn func() error) (err error) {
	lockPath, err := keychainLockPath()
	if err != nil {
		return fmt.Errorf("resolve keychain lock path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create keychain lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open keychain lock: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close keychain lock: %w", closeErr)
		}
	}()

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire keychain lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}

func keychainLockPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", serviceName, "keychain.lock"), nil
}
