package delivery

import "time"

const DLQType = "delivery.dlq"

type DeadLetter struct {
	Type       string `json:"type"`    // "delivery.dlq"
	Version    string `json:"version"` // schema version
	At         string `json:"at"`      // RFC3339 time the DLQ was emitted
	Reason     string `json:"reason"`  // human/debug text
	Attempt    int    `json:"attempt"` // attempt count when DLQ'd
	HTTPStatus int    `json:"http_status,omitempty"`
	LastError  string `json:"last_error,omitempty"`
	Task       Task   `json:"task"` // full delivery snapshot
}

func NewDeadLetter(t Task, attempt, httpStatus int, lastErr, reason string) DeadLetter {
	return DeadLetter{
		Type:       DLQType,
		Version:    "v1",
		At:         time.Now().Format(time.RFC3339Nano),
		Reason:     reason,
		Attempt:    attempt,
		HTTPStatus: httpStatus,
		LastError:  lastErr,
		Task:       t,
	}
}
