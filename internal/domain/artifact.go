package domain

import (
	"net/url"
	"strings"
	"time"
)

// ArtifactDefinition tells an executor which durable deliverable a Workflow Task expects.
// The description guides the work; it does not constrain the deliverable's content type.
type ArtifactDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Validate checks an Artifact Definition.
func (d ArtifactDefinition) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return invalid("artifact_definition.name", "is required")
	}
	if strings.TrimSpace(d.Description) == "" {
		return invalid("artifact_definition.description", "is required for %q", d.Name)
	}
	return nil
}

// Artifact is one immutable deliverable created by a Claim. An Artifact remains
// staged until it is attached to a Submission.
type Artifact struct {
	ID         ArtifactID `json:"id"`
	WorkItemID WorkItemID `json:"work_item_id"`
	TaskID     TaskID     `json:"task_id"`
	ClaimID    ClaimID    `json:"claim_id"`

	SubmissionID *SubmissionID `json:"submission_id"`
	Name         string        `json:"name"`
	URI          string        `json:"uri"`
	CreatedAt    time.Time     `json:"created_at"`
}

// Validate checks Artifact identity, provenance, and location.
func (a Artifact) Validate() error {
	if strings.TrimSpace(string(a.ID)) == "" {
		return invalid("artifact.id", "is required")
	}
	if strings.TrimSpace(string(a.WorkItemID)) == "" || strings.TrimSpace(string(a.TaskID)) == "" || strings.TrimSpace(string(a.ClaimID)) == "" {
		return invalid("artifact", "work item id, task id, and claim id are required")
	}
	if strings.TrimSpace(a.Name) == "" {
		return invalid("artifact.name", "is required")
	}
	location, err := url.Parse(strings.TrimSpace(a.URI))
	if err != nil || location.Scheme == "" {
		return invalid("artifact.uri", "must be an absolute URI")
	}
	if a.CreatedAt.IsZero() {
		return invalid("artifact.created_at", "is required")
	}
	return nil
}

// ArtifactBlob records content managed by a configured Artifact Store.
type ArtifactBlob struct {
	URI       string
	Digest    string
	Size      int64
	CreatedAt time.Time
}

// Validate checks managed blob metadata.
func (b ArtifactBlob) Validate() error {
	location, err := url.Parse(strings.TrimSpace(b.URI))
	if err != nil || location.Scheme == "" {
		return invalid("artifact_blob.uri", "must be an absolute URI")
	}
	if strings.TrimSpace(b.Digest) == "" {
		return invalid("artifact_blob.digest", "is required")
	}
	if b.Size < 0 {
		return invalid("artifact_blob.size", "must not be negative")
	}
	if b.CreatedAt.IsZero() {
		return invalid("artifact_blob.created_at", "is required")
	}
	return nil
}
