package frr

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

const (
	processAccessExecute os.FileMode = 1
	processAccessWrite   os.FileMode = 2
	processAccessRead    os.FileMode = 4
)

func checkPathAccess(path string, required os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return NewPermissionDeniedError("stat "+path, err)
		}
		return err
	}
	if err := checkFileInfoAccess(path, info, required); err != nil {
		return NewPermissionDeniedError("access "+path, err)
	}
	return nil
}

func checkFileInfoAccess(path string, info os.FileInfo, required os.FileMode) error {
	if os.Geteuid() == 0 {
		return nil
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	mode := info.Mode().Perm()
	perms := mode & 0007
	euid := os.Geteuid()
	gids, err := currentGroupIDs()
	if err != nil {
		return err
	}

	if int(stat.Uid) == euid {
		perms = (mode >> 6) & 0007
	} else if containsGroupID(gids, int(stat.Gid)) {
		perms = (mode >> 3) & 0007
	}

	if perms&required == required {
		return nil
	}

	return fmt.Errorf("%s mode=%04o owner uid=%d gid=%d does not allow %s access for uid=%d gids=%v",
		path, mode, stat.Uid, stat.Gid, describeAccess(required), euid, gids)
}

func currentGroupIDs() ([]int, error) {
	gids, err := os.Getgroups()
	if err != nil {
		return nil, fmt.Errorf("inspect process groups: %w", err)
	}
	egid := os.Getegid()
	if !containsGroupID(gids, egid) {
		gids = append(gids, egid)
	}
	return gids, nil
}

func containsGroupID(groups []int, want int) bool {
	for _, group := range groups {
		if group == want {
			return true
		}
	}
	return false
}

func describeAccess(required os.FileMode) string {
	var parts []string
	if required&processAccessRead != 0 {
		parts = append(parts, "read")
	}
	if required&processAccessWrite != 0 {
		parts = append(parts, "write")
	}
	if required&processAccessExecute != 0 {
		parts = append(parts, "execute")
	}
	return strings.Join(parts, "+")
}

func commandFailureError(output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return err
	}
	return fmt.Errorf("%s: %w", detail, err)
}

func commandFailureLooksPermissionDenied(output []byte, err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	detail := strings.ToLower(strings.TrimSpace(string(output)))
	return strings.Contains(detail, "permission denied") ||
		strings.Contains(detail, "operation not permitted") ||
		strings.Contains(detail, "privilege")
}
