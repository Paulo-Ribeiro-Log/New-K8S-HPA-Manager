package helm

import (
	"testing"
	"time"
)

func TestParseHelmTimestamp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		expected time.Time
	}{
		{
			name:     "RFC3339Nano",
			input:    "2026-01-05T20:11:33.950513811Z",
			expected: time.Date(2026, 1, 5, 20, 11, 33, 950513811, time.UTC),
		},
		{
			name:     "RFC3339",
			input:    "2026-01-05T20:11:33Z",
			expected: time.Date(2026, 1, 5, 20, 11, 33, 0, time.UTC),
		},
		{
			name:     "GoLayoutWithZone",
			input:    "2026-01-05 20:11:33 -0300 -03",
			expected: time.Date(2026, 1, 5, 23, 11, 33, 0, time.UTC),
		},
		{
			name:     "Empty",
			input:    "",
			expected: time.Time{},
		},
		{
			name:     "Invalid",
			input:    "not a date",
			expected: time.Time{},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := parseHelmTimestamp(tc.input)
			if !got.Equal(tc.expected) {
				t.Fatalf("expected %v got %v", tc.expected, got)
			}
		})
	}
}

func TestNormalizeStatuses(t *testing.T) {
	t.Parallel()

	t.Run("empty slice returns nil", func(t *testing.T) {
		t.Parallel()
		if normalizeStatuses(nil) != nil {
			t.Fatal("expected nil map for nil input")
		}
		if normalizeStatuses([]string{}) != nil {
			t.Fatal("expected nil map for empty input")
		}
	})

	t.Run("values are trimmed lowercased and deduplicated", func(t *testing.T) {
		t.Parallel()
		input := []string{" Deployed ", "FAILED", "", "deployed"}
		got := normalizeStatuses(input)
		if len(got) != 2 {
			t.Fatalf("expected 2 entries got %d", len(got))
		}
		if !got["deployed"] {
			t.Fatal("expected deployed entry")
		}
		if !got["failed"] {
			t.Fatal("expected failed entry")
		}
	})
}
