package datastore

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type sqliteUnixTime struct {
	unix int64
}

func (t *sqliteUnixTime) Scan(value any) error {
	switch v := value.(type) {
	case int64:
		t.unix = v
		return nil
	case int:
		t.unix = int64(v)
		return nil
	case int32:
		t.unix = int64(v)
		return nil
	case float64:
		t.unix = int64(v)
		return nil
	case []byte:
		return t.scanString(string(v))
	case string:
		return t.scanString(v)
	case time.Time:
		t.unix = v.Unix()
		return nil
	default:
		return fmt.Errorf("unsupported SQLite timestamp type %T", value)
	}
}

func (t *sqliteUnixTime) scanString(value string) error {
	unix, err := parseSQLiteUnixTime(value)
	if err != nil {
		return err
	}
	t.unix = unix
	return nil
}

func (t sqliteUnixTime) Unix() int64 {
	return t.unix
}

func (t sqliteUnixTime) Time() time.Time {
	return time.Unix(t.unix, 0).UTC()
}

func parseSQLiteUnixTime(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("empty SQLite timestamp")
	}

	if unix, err := strconv.ParseInt(value, 10, 64); err == nil {
		return unix, nil
	}

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if ts, err := parseSQLiteTimeLayout(layout, value); err == nil {
			return ts.Unix(), nil
		}
	}

	return 0, fmt.Errorf("unsupported SQLite timestamp value %q", value)
}

func parseSQLiteTimeLayout(layout, value string) (time.Time, error) {
	if strings.Contains(layout, "-07") || strings.Contains(layout, "Z07") || layout == time.RFC3339Nano {
		return time.Parse(layout, value)
	}
	return time.ParseInLocation(layout, value, time.UTC)
}
