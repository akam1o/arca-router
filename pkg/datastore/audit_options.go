package datastore

const (
	defaultAuditEventsLimit = 1000
	maxAuditEventsLimit     = 1000
)

func normalizeAuditOptions(opts *AuditOptions) AuditOptions {
	normalized := AuditOptions{}
	if opts != nil {
		normalized = *opts
	}
	if normalized.Limit <= 0 {
		normalized.Limit = defaultAuditEventsLimit
	} else if normalized.Limit > maxAuditEventsLimit {
		normalized.Limit = maxAuditEventsLimit
	}
	if normalized.Offset < 0 {
		normalized.Offset = 0
	}
	return normalized
}
