package models

import (
	"fmt"
	"time"
)

type Event struct {
	ID        int64
	Content   string
	CreatedAt time.Time
}

func GetEvents(events []Event, from, to time.Time) []string {
	var result []string
	for _, event := range events {
		if event.CreatedAt.After(from) && event.CreatedAt.Before(to) {
			result = append(result, fmt.Sprintf("%s %s", event.CreatedAt, event.Content))
		}
	}

	return result
}
