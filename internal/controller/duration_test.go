package controller

import (
	"testing"
	"time"
)

func TestParseLeaseDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     string
		want      time.Duration
		wantError bool
	}{
		{name: "minutes", value: "30m", want: 30 * time.Minute},
		{name: "compound", value: "1h30m", want: 90 * time.Minute},
		{name: "days", value: "7d", want: 7 * 24 * time.Hour},
		{name: "weeks and days", value: "1w2d", want: 9 * 24 * time.Hour},
		{name: "fractional day", value: "1.5d", want: 36 * time.Hour},
		{name: "zero", value: "0s", wantError: true},
		{name: "negative", value: "-1h", wantError: true},
		{name: "missing unit", value: "30", wantError: true},
		{name: "unsupported unit", value: "1y", wantError: true},
		{name: "space", value: "1d 2h", wantError: true},
		{name: "too large", value: "999999999999999999999w", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseLeaseDuration(test.value)
			if test.wantError {
				if err == nil {
					t.Fatalf("ParseLeaseDuration(%q) error=nil, want an error", test.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLeaseDuration(%q) error=%v, want nil", test.value, err)
			}
			if got != test.want {
				t.Fatalf("ParseLeaseDuration(%q)=%s, want %s", test.value, got, test.want)
			}
		})
	}
}

func TestFormatLeaseDuration(t *testing.T) {
	t.Parallel()

	if got := FormatLeaseDuration(30 * 24 * time.Hour); got != "30d" {
		t.Fatalf("FormatLeaseDuration(30d)=%q, want 30d", got)
	}
	if got := FormatLeaseDuration(90 * time.Minute); got != "1h30m0s" {
		t.Fatalf("FormatLeaseDuration(90m)=%q, want 1h30m0s", got)
	}
}
