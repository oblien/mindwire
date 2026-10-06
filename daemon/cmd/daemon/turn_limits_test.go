package main

import (
	"testing"
	"time"
)

func TestConfiguredTurnTimeout(t *testing.T) {
	for _, test := range []struct {
		value   string
		want    time.Duration
		invalid bool
	}{
		{"", 0, false}, {"0", 0, false}, {" 0s ", 0, false},
		{"72h", 72 * time.Hour, false}, {"576h", 24 * 24 * time.Hour, false},
		{"-1h", 0, true}, {"forever", 0, true}, {"24", 0, true},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := configuredTurnTimeout(test.value)
			if got != test.want || (err != nil) != test.invalid {
				t.Fatalf("got %v, %v; want %v, invalid=%v", got, err, test.want, test.invalid)
			}
		})
	}
}
