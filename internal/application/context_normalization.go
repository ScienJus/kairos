package application

import "github.com/ScienJus/kairos/internal/domain"

func normalizeWorkItemCollections(workItem domain.WorkItem) domain.WorkItem {
	if workItem.Tags == nil {
		workItem.Tags = []string{}
	}
	return workItem
}

func normalizeTaskCollections(task domain.Task) domain.Task {
	if task.AllowedRoles == nil {
		task.AllowedRoles = []string{}
	}
	if task.Tags == nil {
		task.Tags = []string{}
	}
	if task.Reviews == nil {
		task.Reviews = []domain.Review{}
	}
	if task.Submissions == nil {
		task.Submissions = []domain.TaskSubmission{}
	}
	if task.Failures == nil {
		task.Failures = []domain.TaskFailure{}
	}
	if task.TransitionDecisions == nil {
		task.TransitionDecisions = []domain.TransitionDecision{}
	}
	return task
}

func normalizeTasks(tasks []domain.Task) []domain.Task {
	if tasks == nil {
		return []domain.Task{}
	}
	for index := range tasks {
		tasks[index] = normalizeTaskCollections(tasks[index])
	}
	return tasks
}

func normalizeBlackboardTaskDecomposition(decomposition BlackboardTaskDecomposition) BlackboardTaskDecomposition {
	decomposition.Parent = normalizeTaskCollections(decomposition.Parent)
	decomposition.Children = normalizeTasks(decomposition.Children)
	return decomposition
}

func normalizeDefinitionContext(context DefinitionExecutionContext) DefinitionExecutionContext {
	if context.SuggestedTags == nil {
		context.SuggestedTags = []string{}
	}
	return context
}

func normalizeDefinitionMetadata(metadata domain.DefinitionMetadata) domain.DefinitionMetadata {
	metadata.SuggestedTags = append([]string{}, metadata.SuggestedTags...)
	return metadata
}

func normalizeWorkflowDefinition(definition domain.WorkflowDefinition) domain.WorkflowDefinition {
	definition.DefinitionMetadata = normalizeDefinitionMetadata(definition.DefinitionMetadata)
	definition.Graph.StartTaskIDs = append([]domain.WorkflowTaskID{}, definition.Graph.StartTaskIDs...)
	tasks := make([]domain.WorkflowTaskDefinition, len(definition.Graph.Tasks))
	for index := range definition.Graph.Tasks {
		tasks[index] = normalizeWorkflowTaskDefinition(definition.Graph.Tasks[index])
	}
	definition.Graph.Tasks = tasks
	definition.Graph.Relations = append([]domain.WorkflowRelationDefinition{}, definition.Graph.Relations...)
	return definition
}

func normalizeWorkflowDefinitions(definitions []domain.WorkflowDefinition) []domain.WorkflowDefinition {
	if definitions == nil {
		return []domain.WorkflowDefinition{}
	}
	for index := range definitions {
		definitions[index] = normalizeWorkflowDefinition(definitions[index])
	}
	return definitions
}

func normalizeBlackboardDefinition(definition domain.BlackboardDefinition) domain.BlackboardDefinition {
	definition.DefinitionMetadata = normalizeDefinitionMetadata(definition.DefinitionMetadata)
	return definition
}

func normalizeBlackboardDefinitions(definitions []domain.BlackboardDefinition) []domain.BlackboardDefinition {
	if definitions == nil {
		return []domain.BlackboardDefinition{}
	}
	for index := range definitions {
		definitions[index] = normalizeBlackboardDefinition(definitions[index])
	}
	return definitions
}

func normalizeWorkflowTaskDefinition(task domain.WorkflowTaskDefinition) domain.WorkflowTaskDefinition {
	task.AllowedRoles = append([]string{}, task.AllowedRoles...)
	task.DefaultTags = append([]string{}, task.DefaultTags...)
	task.Artifacts = append([]domain.ArtifactDefinition{}, task.Artifacts...)
	return task
}

func normalizeWorkflowChoiceOptions(options []WorkflowChoiceOption) []WorkflowChoiceOption {
	if options == nil {
		return []WorkflowChoiceOption{}
	}
	for optionIndex := range options {
		if options[optionIndex].Targets == nil {
			options[optionIndex].Targets = []domain.WorkflowTaskDefinition{}
		}
		for taskIndex := range options[optionIndex].Targets {
			options[optionIndex].Targets[taskIndex] = normalizeWorkflowTaskDefinition(options[optionIndex].Targets[taskIndex])
		}
		if options[optionIndex].Relations == nil {
			options[optionIndex].Relations = []WorkflowChoiceRelation{}
		}
		for relationIndex := range options[optionIndex].Relations {
			options[optionIndex].Relations[relationIndex].Target = normalizeWorkflowTaskDefinition(options[optionIndex].Relations[relationIndex].Target)
		}
		if options[optionIndex].SkippableOptionalTasks == nil {
			options[optionIndex].SkippableOptionalTasks = []domain.WorkflowTaskDefinition{}
		}
		for taskIndex := range options[optionIndex].SkippableOptionalTasks {
			options[optionIndex].SkippableOptionalTasks[taskIndex] = normalizeWorkflowTaskDefinition(options[optionIndex].SkippableOptionalTasks[taskIndex])
		}
	}
	return options
}
