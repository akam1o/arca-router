package frr

import (
	"errors"
	"fmt"
	"os"
)

var (
	vtyshPathCandidates = []string{
		"/usr/bin/vtysh",
		"/usr/sbin/vtysh",
		"/usr/lib/frr/vtysh",
	}
	ipPathCandidates = []string{
		"/usr/sbin/ip",
		"/sbin/ip",
		"/usr/bin/ip",
		"/bin/ip",
	}
)

func lookupVtyshPath() (string, error) {
	return lookupSystemExecutable("vtysh", vtyshPathCandidates)
}

func lookupIPPath() (string, error) {
	return lookupSystemExecutable("ip", ipPathCandidates)
}

func lookupSystemExecutable(tool string, candidates []string) (string, error) {
	var firstErr error
	var permissionErr error
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if errors.Is(err, os.ErrPermission) {
				if permissionErr == nil {
					permissionErr = fmt.Errorf("stat %s: %w", candidate, err)
				}
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("stat %s: %w", candidate, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Mode().Perm()&0111 == 0 {
			if permissionErr == nil {
				permissionErr = fmt.Errorf("%s is not executable: %w", candidate, os.ErrPermission)
			}
			continue
		}
		return candidate, nil
	}
	if permissionErr != nil {
		return "", permissionErr
	}
	if firstErr != nil {
		return "", firstErr
	}
	return "", fmt.Errorf("%s not found in trusted paths: %w", tool, os.ErrNotExist)
}
