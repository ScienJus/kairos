import { useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Background, Controls, MarkerType, Position, ReactFlow, type Edge, type Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ChevronDown, ChevronRight, ChevronsDown, ChevronsUp, CircleDot } from 'lucide-react'
import { useI18n } from './i18n'
import type { Task, TaskRelation, WorkflowDefinition } from './types'
import { Status } from './ui'
import { workflowEdgePresentation, workflowEdgeTypes, workflowGraphLayout, workflowNodeIsSelected, workflowNodeTypes } from './WorkflowGraphEdges'

export function TaskMap({ mode, tasks, relations, workflowDefinition, selectedTaskID, onSelect }: { mode: string; tasks: Task[]; relations: TaskRelation[]; workflowDefinition?: WorkflowDefinition; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const { t } = useI18n()
  const [hideCompleted, setHideCompleted] = useState(false)
  const [collapsedTaskIDs, setCollapsedTaskIDs] = useState<Set<string>>(() => new Set())
  if (tasks.length === 0) return mode === 'blackboard' ? null : <div className="empty-task"><CircleDot size={19} /><span>{t('awaitingPlanning')}</span></div>
  if (mode === 'workflow') return <WorkflowGraph tasks={tasks} relations={relations} definition={workflowDefinition} selectedTaskID={selectedTaskID} onSelect={onSelect} />
  const parentByID = new Map(tasks.map(task => [task.id, task.parent_task_id]))
  const depthOf = (taskID: string, seen = new Set<string>()): number => {
    const parentID = parentByID.get(taskID)
    if (!parentID || seen.has(taskID)) return 0
    seen.add(taskID)
    return 1 + depthOf(parentID, seen)
  }
  const children = new Map<string | null, Task[]>()
  tasks.forEach(task => {
    const siblings = children.get(task.parent_task_id) ?? []
    siblings.push(task)
    children.set(task.parent_task_id, siblings)
  })
  const ordered: Task[] = []
  const appendChildren = (parentID: string | null) => (children.get(parentID) ?? []).forEach(task => { ordered.push(task); appendChildren(task.id) })
  appendChildren(null)
  const parentTaskIDs = ordered.filter(task => (children.get(task.id)?.length ?? 0) > 0).map(task => task.id)
  const hiddenByCollapsedParent = (task: Task) => {
    let parentID = task.parent_task_id
    while (parentID) {
      if (collapsedTaskIDs.has(parentID)) return true
      parentID = parentByID.get(parentID) ?? null
    }
    return false
  }
  const visibleTasks = ordered.filter(task => !hiddenByCollapsedParent(task) && (!hideCompleted || (task.status !== 'completed' && task.status !== 'skipped')))
  const setCollapsed = (taskID: string, collapsed: boolean) => setCollapsedTaskIDs(current => {
    const next = new Set(current)
    if (collapsed) next.add(taskID); else next.delete(taskID)
    return next
  })
  return <><div className="checklist-toolbar"><span>{hideCompleted && visibleTasks.length === 0 ? t('completedHidden') : ''}</span><div className="checklist-controls"><button title={t('collapseAll')} aria-label={t('collapseAll')} onClick={() => setCollapsedTaskIDs(new Set(parentTaskIDs))}><ChevronsUp size={15} /><span>{t('collapseAll')}</span></button><button title={t('expandAll')} aria-label={t('expandAll')} onClick={() => setCollapsedTaskIDs(new Set())}><ChevronsDown size={15} /><span>{t('expandAll')}</span></button><label className="switch-control"><span>{t('hideCompleted')}</span><input type="checkbox" checked={hideCompleted} onChange={event => setHideCompleted(event.target.checked)} /><i aria-hidden="true" /></label></div></div><div className="checklist" role="list">{visibleTasks.map(task => {
    const checked = task.status === 'completed' || task.status === 'skipped'
    const select = () => onSelect(task.id)
    const hasChildren = (children.get(task.id)?.length ?? 0) > 0
    const collapsed = collapsedTaskIDs.has(task.id)
    return <div key={task.id} role="listitem" tabIndex={0} style={{ paddingLeft: `${15 + depthOf(task.id) * 24}px` }} className={`check-row depth-${depthOf(task.id)} ${selectedTaskID === task.id ? 'selected' : ''} ${checked ? 'checked' : ''}`} onClick={select} onKeyDown={(event: ReactKeyboardEvent<HTMLDivElement>) => { if (event.key === 'Enter' || event.key === ' ') select() }}>
      {hasChildren ? <button className="tree-toggle" title={t(collapsed ? 'expandTask' : 'collapseTask')} aria-label={t(collapsed ? 'expandTask' : 'collapseTask')} onClick={event => { event.stopPropagation(); setCollapsed(task.id, !collapsed) }}>{collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}</button> : <span className="tree-toggle-placeholder" />}
      <input type="checkbox" checked={checked} readOnly tabIndex={-1} aria-label={task.title} />
      <span className="check-content"><strong>{task.title}</strong>{task.tags.length > 0 && <span className="task-tags compact">{task.tags.slice(0, 2).map(tag => <span key={tag}>{tag}</span>)}{task.tags.length > 2 && <span className="tag-overflow">+{task.tags.length - 2}</span>}</span>}<small>{task.description || t('taskFallback', { executor: t(task.executor) })}</small></span>
      <Status value={task.status} />
    </div>
  })}</div></>
}

function WorkflowGraph({ tasks, relations, definition, selectedTaskID, onSelect }: { tasks: Task[]; relations: TaskRelation[]; definition?: WorkflowDefinition; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const { t } = useI18n()
  const runtimeByDefinition = new Map<string, Task[]>()
  tasks.forEach(task => { if (task.workflow_task_id) runtimeByDefinition.set(task.workflow_task_id, [...(runtimeByDefinition.get(task.workflow_task_id) ?? []), task]) })
  runtimeByDefinition.forEach(runtime => runtime.sort((left, right) => left.position - right.position))
  if (!definition) return <RuntimeWorkflowGraph tasks={tasks} relations={relations} selectedTaskID={selectedTaskID} onSelect={onSelect} />
  const definitions = definition.graph.tasks
  const layout = workflowGraphLayout(definitions.map(task => task.id), definition.graph.relations, definition.graph.start_task_ids)
  const columnGap = definition.graph.relations.some(relation => relation.label?.trim()) ? 330 : 250
  const rows = new Map<number, number>()
  const nodes: Node[] = definitions.map((task, index) => {
    const runtime = runtimeByDefinition.get(task.id) ?? []; const latest = runtime.at(-1); const status = latest?.status ?? 'not_reached'; const column = layout.depths.get(task.id) ?? index; const row = rows.get(column) ?? 0; rows.set(column, row + 1)
    const role = task.allowed_roles.length > 0 ? task.allowed_roles.join(', ') : t('unrestricted')
    return { id: task.id, position: { x: column * columnGap, y: row * 96 }, data: { label: <div className="flow-node-content"><Status value={status} /><strong>{task.title}</strong><small>{t(task.executor)} · {role}{runtime.length > 1 ? ` · ${t('workflowRuns', { count: runtime.length })}` : ''}</small></div> }, type: 'workflowTask', className: `flow-node status-node-${status} ${workflowNodeIsSelected(runtime.map(instance => instance.id), selectedTaskID) ? 'selected' : ''}`, sourcePosition: Position.Right, targetPosition: Position.Left }
  })
  const edges: Edge[] = definition.graph.relations.map(relation => {
    const presentation = workflowEdgePresentation(relation.from_task_id, relation.to_task_id, layout.isBackEdge(relation.from_task_id, relation.to_task_id))
    return { id: relation.id, source: relation.from_task_id, target: relation.to_task_id, markerEnd: { type: MarkerType.ArrowClosed }, className: presentation.type === 'smoothstep' ? '' : 'workflow-cycle-edge', ...presentation, label: relation.label || undefined, labelClassName: 'workflow-edge-label', labelBgPadding: [7, 4], labelBgBorderRadius: 5 }
  })
  return <div className="workflow-graph"><ReactFlow nodes={nodes} edges={edges} edgeTypes={workflowEdgeTypes} nodeTypes={workflowNodeTypes} fitView fitViewOptions={{ padding: .25 }} minZoom={.5} maxZoom={1.4} nodesDraggable={false} nodesConnectable={false} elementsSelectable proOptions={{ hideAttribution: true }} onNodeClick={(_, node) => { const latest = runtimeByDefinition.get(node.id)?.at(-1); if (latest) onSelect(latest.id) }}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}

function RuntimeWorkflowGraph({ tasks, relations, selectedTaskID, onSelect }: { tasks: Task[]; relations: TaskRelation[]; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const nodes: Node[] = tasks.map((task, index) => ({ id: task.id, position: { x: (task.position ?? index) * 250, y: 0 }, data: { label: <div className="flow-node-content"><Status value={task.status} /><strong>{task.title}</strong></div> }, className: `flow-node status-node-${task.status} ${selectedTaskID === task.id ? 'selected' : ''}`, sourcePosition: Position.Right, targetPosition: Position.Left }))
  const ids = new Set(tasks.map(task => task.id)); const edges: Edge[] = relations.filter(relation => ids.has(relation.from_task_id) && ids.has(relation.to_task_id)).map(relation => ({ id: `${relation.from_task_id}-${relation.to_task_id}`, source: relation.from_task_id, target: relation.to_task_id, markerEnd: { type: MarkerType.ArrowClosed }, type: 'smoothstep' }))
  return <div className="workflow-graph"><ReactFlow nodes={nodes} edges={edges} fitView nodesDraggable={false} nodesConnectable={false} proOptions={{ hideAttribution: true }} onNodeClick={(_, node) => onSelect(node.id)}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}
