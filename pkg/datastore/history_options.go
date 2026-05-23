package datastore

const (
	defaultCommitHistoryLimit = 1000
	maxCommitHistoryLimit     = 1000
)

func normalizeHistoryOptions(opts *HistoryOptions) HistoryOptions {
	normalized := HistoryOptions{}
	if opts != nil {
		normalized = *opts
	}
	if normalized.Limit <= 0 {
		normalized.Limit = defaultCommitHistoryLimit
	} else if normalized.Limit > maxCommitHistoryLimit {
		normalized.Limit = maxCommitHistoryLimit
	}
	if normalized.Offset < 0 {
		normalized.Offset = 0
	}
	return normalized
}
