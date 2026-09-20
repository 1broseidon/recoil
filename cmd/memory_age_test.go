package cmd

import (
	"math"
	"testing"
	"time"

	"github.com/1broseidon/recoil/internal/store"
)

func TestRecencyPrior(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		mem  store.Memory
		want float64
	}{
		{
			name: "fresh direct gets max prior",
			mem:  store.Memory{SourceKind: " direct ", CreatedAt: now.Format(time.RFC3339)},
			want: recencyPriorMax,
		},
		{
			name: "old direct gets no prior",
			mem:  store.Memory{SourceKind: "direct", CreatedAt: now.AddDate(0, 0, -91).Format(time.RFC3339)},
			want: 0,
		},
		{
			name: "fresh file gets no prior",
			mem:  store.Memory{SourceKind: "file", CreatedAt: now.Format(time.RFC3339)},
			want: 0,
		},
		{
			name: "unparseable created_at gets no prior",
			mem:  store.Memory{SourceKind: "session_evidence", CreatedAt: "not-a-time"},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recencyPrior(tt.mem, now)
			if math.Abs(got-tt.want) > 0.0001 {
				t.Fatalf("recencyPrior() = %f, want %f", got, tt.want)
			}
		})
	}
}
