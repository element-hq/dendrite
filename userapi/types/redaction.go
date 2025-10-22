package types

import "time"

// RedactionJobStatus describes the lifecycle status of a user redaction job.
type RedactionJobStatus string

const (
	// RedactionJobStatusPending indicates the job is queued for processing.
	RedactionJobStatusPending RedactionJobStatus = "pending"
	// RedactionJobStatusCompleted indicates the job has finished successfully.
	RedactionJobStatusCompleted RedactionJobStatus = "completed"
	// RedactionJobStatusFailed indicates the job failed permanently.
	RedactionJobStatusFailed RedactionJobStatus = "failed"
)

// UserRedactionJob models a queued request to redact a user's historical content.
type UserRedactionJob struct {
	JobID          int64
	UserID         string
	RequestedBy    string
	RequestedAt    time.Time
	Status         RedactionJobStatus
	RedactMessages bool
}
