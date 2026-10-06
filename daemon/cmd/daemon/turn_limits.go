package main

import (
	"fmt"
	"strings"
	"time"
)

func configuredTurnTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return 0, fmt.Errorf("TURN_TIMEOUT must be 0 (unlimited) or a positive duration such as 72h")
	}
	return duration, nil
}
