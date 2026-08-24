package domain

import (
	"strings"
	"time"
)

// DefinitionStatus is the publication state of a coordination definition version.
type DefinitionStatus string

const (
	DefinitionStatusDraft     DefinitionStatus = "draft"
	DefinitionStatusPublished DefinitionStatus = "published"
	DefinitionStatusArchived  DefinitionStatus = "archived"
)

// Valid reports whether the definition status is recognized.
func (s DefinitionStatus) Valid() bool {
	return s == DefinitionStatusDraft || s == DefinitionStatusPublished || s == DefinitionStatusArchived
}

// DefinitionBinding identifies the immutable definition version used by a WorkItem.
type DefinitionBinding struct {
	ID      DefinitionID     `json:"id"`
	Version int64            `json:"version"`
	Mode    CoordinationMode `json:"mode"`
}

// Validate checks the DefinitionBinding invariants.
func (b DefinitionBinding) Validate() error {
	if strings.TrimSpace(string(b.ID)) == "" {
		return invalid("definition.id", "is required")
	}
	if b.Version <= 0 {
		return invalid("definition.version", "must be greater than zero")
	}
	if !b.Mode.Valid() {
		return invalid("definition.mode", "unsupported value %q", b.Mode)
	}
	return nil
}

// DefinitionMetadata contains versioned configuration shared by Workflow and Blackboard.
type DefinitionMetadata struct {
	ID      DefinitionID `json:"id"`
	Version int64        `json:"version"`

	Name        string `json:"name"`
	Description string `json:"description"`

	// AgentInstructions provides definition-level collaboration guidance.
	AgentInstructions string `json:"agent_instructions"`

	// SuggestedTags describes the recommended tag vocabulary. Entries may be
	// concrete tags or open patterns such as "module:*". They guide Agents and
	// are not enforced as a schema or permission rule.
	SuggestedTags []string `json:"suggested_tags"`

	Status DefinitionStatus `json:"status"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the shared DefinitionMetadata invariants.
func (m DefinitionMetadata) Validate() error {
	if strings.TrimSpace(string(m.ID)) == "" {
		return invalid("definition.id", "is required")
	}
	if m.Version <= 0 {
		return invalid("definition.version", "must be greater than zero")
	}
	if strings.TrimSpace(m.Name) == "" {
		return invalid("definition.name", "is required")
	}
	if !m.Status.Valid() {
		return invalid("definition.status", "unsupported value %q", m.Status)
	}
	if err := validateStringSet("definition.suggested_tags", m.SuggestedTags); err != nil {
		return err
	}
	return validateTimestamps(m.CreatedAt, m.UpdatedAt)
}

// Binding returns a reference to this metadata for the given coordination mode.
func (m DefinitionMetadata) Binding(mode CoordinationMode) DefinitionBinding {
	return DefinitionBinding{ID: m.ID, Version: m.Version, Mode: mode}
}

// BlackboardDefinition configures a shared Blackboard collaboration space.
type BlackboardDefinition struct {
	DefinitionMetadata
}

// Validate checks the BlackboardDefinition invariants.
func (d BlackboardDefinition) Validate() error {
	return d.DefinitionMetadata.Validate()
}

// Binding returns the immutable reference stored by Blackboard WorkItems.
func (d BlackboardDefinition) Binding() DefinitionBinding {
	return d.DefinitionMetadata.Binding(CoordinationModeBlackboard)
}

// WorkflowDefinition configures a versioned Workflow collaboration space.
type WorkflowDefinition struct {
	DefinitionMetadata
	Graph WorkflowGraph `json:"graph"`
}

// Validate checks the WorkflowDefinition invariants.
func (d WorkflowDefinition) Validate() error {
	if err := d.DefinitionMetadata.Validate(); err != nil {
		return err
	}
	if d.Status != DefinitionStatusDraft {
		return d.Graph.Validate()
	}
	return nil
}

// Binding returns the immutable reference stored by Workflow WorkItems.
func (d WorkflowDefinition) Binding() DefinitionBinding {
	return d.DefinitionMetadata.Binding(CoordinationModeWorkflow)
}

// CompileGraph validates and compiles the graph into runtime choice groups.
func (d WorkflowDefinition) CompileGraph() (CompiledWorkflowGraph, error) {
	return d.Graph.Compile()
}
