package datastore

import "testing"

func TestNormalizeAuditOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       *AuditOptions
		wantLimit  int
		wantOffset int
	}{
		{
			name:      "nil uses default limit",
			wantLimit: defaultAuditEventsLimit,
		},
		{
			name:      "zero limit uses default",
			opts:      &AuditOptions{},
			wantLimit: defaultAuditEventsLimit,
		},
		{
			name:      "negative limit uses default",
			opts:      &AuditOptions{Limit: -1},
			wantLimit: defaultAuditEventsLimit,
		},
		{
			name:      "oversized limit is capped",
			opts:      &AuditOptions{Limit: maxAuditEventsLimit + 1},
			wantLimit: maxAuditEventsLimit,
		},
		{
			name:       "negative offset is clamped",
			opts:       &AuditOptions{Limit: 10, Offset: -1},
			wantLimit:  10,
			wantOffset: 0,
		},
		{
			name:       "valid pagination is preserved",
			opts:       &AuditOptions{Limit: 10, Offset: 5},
			wantLimit:  10,
			wantOffset: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeAuditOptions(tt.opts)
			if got.Limit != tt.wantLimit {
				t.Fatalf("Limit = %d, want %d", got.Limit, tt.wantLimit)
			}
			if got.Offset != tt.wantOffset {
				t.Fatalf("Offset = %d, want %d", got.Offset, tt.wantOffset)
			}
		})
	}
}
