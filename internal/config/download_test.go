package config

import (
	"errors"
	"testing"
)

func ptr(n int) *int { return &n }

func TestSetDownloadLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   int
		enabled  bool
		mbit     *int
		wantErr  bool
		wantOn   bool
		wantMbit int
	}{
		{"switch on with a rate", 0, true, ptr(50), false, true, 50},
		{"lowest rate", 0, true, ptr(MinDownloadMbit), false, true, MinDownloadMbit},
		{"highest rate", 0, true, ptr(MaxDownloadMbit), false, true, MaxDownloadMbit},
		{"zero rate", 0, true, ptr(0), true, false, 0},
		{"negative rate", 0, false, ptr(-1), true, false, 0},
		{"rate too high", 0, true, ptr(MaxDownloadMbit + 1), true, false, 0},
		{"switch on without any rate", 0, true, nil, true, false, 0},
		{"switch on keeps the stored rate", 20, true, nil, false, true, 20},
		{"switch off keeps the stored rate", 20, false, nil, false, false, 20},
		{"switch off with a new rate", 20, false, ptr(30), false, false, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{DownloadLimitMbit: tc.stored}
			err := c.SetDownloadLimit(tc.enabled, tc.mbit)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidDownloadLimit) {
					t.Fatalf("err = %v, want ErrInvalidDownloadLimit", err)
				}
				if c.DownloadLimitEnabled || c.DownloadLimitMbit != tc.stored {
					t.Errorf("config changed on error: %+v", c)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if on, mbit := NewView(&c).DownloadLimit(); on != tc.wantOn || mbit != tc.wantMbit {
				t.Errorf("DownloadLimit() = %v, %d; want %v, %d", on, mbit, tc.wantOn, tc.wantMbit)
			}
		})
	}
}

func TestValidateDownloadLimit(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		mbit    int
		ok      bool
	}{
		{false, 0, true},
		{true, 10, true},
		{false, 10, true},
		{true, 0, false},
		{false, -5, false},
		{true, MaxDownloadMbit + 1, false},
	} {
		c := Config{DownloadLimitEnabled: tc.enabled, DownloadLimitMbit: tc.mbit}
		if err := c.Validate(); (err == nil) != tc.ok {
			t.Errorf("Validate(enabled=%v, mbit=%d) = %v, want ok=%v", tc.enabled, tc.mbit, err, tc.ok)
		}
	}
}
