package datastore

import "testing"

func TestNormalizeHistoryOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       *HistoryOptions
		wantLimit  int
		wantOffset int
	}{
		{
			name:      "nil uses default limit",
			wantLimit: defaultCommitHistoryLimit,
		},
		{
			name:      "zero limit uses default",
			opts:      &HistoryOptions{},
			wantLimit: defaultCommitHistoryLimit,
		},
		{
			name:      "negative limit uses default",
			opts:      &HistoryOptions{Limit: -1},
			wantLimit: defaultCommitHistoryLimit,
		},
		{
			name:      "oversized limit is capped",
			opts:      &HistoryOptions{Limit: maxCommitHistoryLimit + 1},
			wantLimit: maxCommitHistoryLimit,
		},
		{
			name:       "negative offset is clamped",
			opts:       &HistoryOptions{Limit: 10, Offset: -1},
			wantLimit:  10,
			wantOffset: 0,
		},
		{
			name:       "valid pagination is preserved",
			opts:       &HistoryOptions{Limit: 10, Offset: 5},
			wantLimit:  10,
			wantOffset: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeHistoryOptions(tt.opts)
			if got.Limit != tt.wantLimit {
				t.Fatalf("Limit = %d, want %d", got.Limit, tt.wantLimit)
			}
			if got.Offset != tt.wantOffset {
				t.Fatalf("Offset = %d, want %d", got.Offset, tt.wantOffset)
			}
		})
	}
}
