import { useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { Background, Controls, MarkerType, Position, ReactFlow, type Edge, type Node } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { ChevronDown, ChevronRight, ChevronsDown, ChevronsUp, CircleDot } from 'lucide-react'
import { useI18n } from './i18n'
import type { Task, TaskRelation, WorkflowDefinition } from './types'
import { Status } from './ui'

export function TaskMap({ mode, tasks, relations, workflowDefinition, selectedTaskID, onSelect }: { mode: string; tasks: Task[]; relations: TaskRelation[]; workflowDefinition?: WorkflowDefinition; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const { t } = useI18n()
  const [hideCompleted, setHideCompleted] = useState(false)
  const [collapsedTaskIDs, setCollapsedTaskIDs] = useState<Set<string>>(() => new Set())
  if (tasks.length === 0) return mode === 'blackboard' ? null : <div className="empty-task"><CircleDot size={19} /><span>{t('awaitingPlanning')}</span></div>
  if (mode === 'workflow') return <WorkflowGraph tasks={tasks} relations={relations} definition={workflowDefinition} selectedTaskID={selectedTaskID} onSelect={onSelect} />
  const parentByID = new Map(tasks.map(task => [task.ID, task.ParentTaskID]))
  const depthOf = (taskID: string, seen = new Set<string>()): number => {
    const parentID = parentByID.get(taskID)
    if (!parentID || seen.has(taskID)) return 0
    seen.add(taskID)
    return 1 + depthOf(parentID, seen)
  }
  const children = new Map<string | null, Task[]>()
  tasks.forEach(task => {
    const siblings = children.get(task.ParentTaskID) ?? []
    siblings.push(task)
    children.set(task.ParentTaskID, siblings)
  })
  const ordered: Task[] = []
  const appendChildren = (parentID: string | null) => (children.get(parentID) ?? []).forEach(task => { ordered.push(task); appendChildren(task.ID) })
  appendChildren(null)
  const parentTaskIDs = ordered.filter(task => (children.get(task.ID)?.length ?? 0) > 0).map(task => task.ID)
  const hiddenByCollapsedParent = (task: Task) => {
    let parentID = task.ParentTaskID
    while (parentID) {
      if (collapsedTaskIDs.has(parentID)) return true
      parentID = parentByID.get(parentID) ?? null
    }
    return false
  }
  const visibleTasks = ordered.filter(task => !hiddenByCollapsedParent(task) && (!hideCompleted || (task.Status !== 'completed' && task.Status !== 'skipped')))
  const setCollapsed = (taskID: string, collapsed: boolean) => setCollapsedTaskIDs(current => {
    const next = new Set(current)
    if (collapsed) next.add(taskID); else next.delete(taskID)
    return next
  })
  return <><div className="checklist-toolbar"><span>{hideCompleted && visibleTasks.length === 0 ? t('completedHidden') : ''}</span><div className="checklist-controls"><button title={t('collapseAll')} aria-label={t('collapseAll')} onClick={() => setCollapsedTaskIDs(new Set(parentTaskIDs))}><ChevronsUp size={15} /><span>{t('collapseAll')}</span></button><button title={t('expandAll')} aria-label={t('expandAll')} onClick={() => setCollapsedTaskIDs(new Set())}><ChevronsDown size={15} /><span>{t('expandAll')}</span></button><label className="switch-control"><span>{t('hideCompleted')}</span><input type="checkbox" checked={hideCompleted} onChange={event => setHideCompleted(event.target.checked)} /><i aria-hidden="true" /></label></div></div><div className="checklist" role="list">{visibleTasks.map(task => {
    const checked = task.Status === 'completed' || task.Status === 'skipped'
    const select = () => onSelect(task.ID)
    const hasChildren = (children.get(task.ID)?.length ?? 0) > 0
    const collapsed = collapsedTaskIDs.has(task.ID)
    return <div key={task.ID} role="listitem" tabIndex={0} style={{ paddingLeft: `${15 + depthOf(task.ID) * 24}px` }} className={`check-row depth-${depthOf(task.ID)} ${selectedTaskID === task.ID ? 'selected' : ''} ${checked ? 'checked' : ''}`} onClick={select} onKeyDown={(event: ReactKeyboardEvent<HTMLDivElement>) => { if (event.key === 'Enter' || event.key === ' ') select() }}>
      {hasChildren ? <button className="tree-toggle" title={t(collapsed ? 'expandTask' : 'collapseTask')} aria-label={t(collapsed ? 'expandTask' : 'collapseTask')} onClick={event => { event.stopPropagation(); setCollapsed(task.ID, !collapsed) }}>{collapsed ? <ChevronRight size={16} /> : <ChevronDown size={16} />}</button> : <span className="tree-toggle-placeholder" />}
      <input type="checkbox" checked={checked} readOnly tabIndex={-1} aria-label={task.Title} />
      <span className="check-content"><strong>{task.Title}</strong>{task.Tags.length > 0 && <span className="task-tags compact">{task.Tags.slice(0, 2).map(tag => <span key={tag}>{tag}</span>)}{task.Tags.length > 2 && <span className="tag-overflow">+{task.Tags.length - 2}</span>}</span>}<small>{task.Description || t('taskFallback', { executor: t(task.Executor) })}</small></span>
      <Status value={task.Status} />
    </div>
  })}</div></>
}

function WorkflowGraph({ tasks, relations, definition, selectedTaskID, onSelect }: { tasks: Task[]; relations: TaskRelation[]; definition?: WorkflowDefinition; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const { t } = useI18n()
  const runtimeByDefinition = new Map<string, Task[]>()
  tasks.forEach(task => { if (task.WorkflowTaskID) runtimeByDefinition.set(task.WorkflowTaskID, [...(runtimeByDefinition.get(task.WorkflowTaskID) ?? []), task]) })
  if (!definition) return <RuntimeWorkflowGraph tasks={tasks} relations={relations} selectedTaskID={selectedTaskID} onSelect={onSelect} />
  const definitions = definition.Graph.Tasks
  const incoming = new Map(definitions.map(task => [task.ID, 0]))
  definition.Graph.Relations.forEach(relation => incoming.set(relation.ToTaskID, (incoming.get(relation.ToTaskID) ?? 0) + 1))
  const depth = new Map<string, number>(); const queue: string[] = []
  const startIDs = definition.Graph.StartTaskIDs; startIDs.forEach(id => depth.set(id, 0)); queue.splice(0, queue.length, ...startIDs)
  while (queue.length) {
    const current = queue.shift()!; const currentDepth = depth.get(current) ?? 0
    definition.Graph.Relations.filter(relation => relation.FromTaskID === current).forEach(relation => { depth.set(relation.ToTaskID, Math.max(depth.get(relation.ToTaskID) ?? 0, currentDepth + 1)); incoming.set(relation.ToTaskID, (incoming.get(relation.ToTaskID) ?? 1) - 1); if (incoming.get(relation.ToTaskID) === 0) queue.push(relation.ToTaskID) })
  }
  const rows = new Map<number, number>()
  const nodes: Node[] = definitions.map((task, index) => {
    const runtime = runtimeByDefinition.get(task.ID) ?? []; const latest = runtime.at(-1); const status = latest?.Status ?? 'not_reached'; const column = depth.get(task.ID) ?? index; const row = rows.get(column) ?? 0; rows.set(column, row + 1)
    const role = task.AllowedRoles.length > 0 ? task.AllowedRoles.join(', ') : t('unrestricted')
    return { id: task.ID, position: { x: column * 250, y: row * 96 }, data: { label: <div className="flow-node-content"><Status value={status} /><strong>{task.Title}</strong><small>{t(task.Executor)} · {role}{runtime.length > 1 ? ` · ${runtime.length} runs` : ''}</small></div> }, className: `flow-node status-node-${status} ${latest && selectedTaskID === latest.ID ? 'selected' : ''}`, sourcePosition: Position.Right, targetPosition: Position.Left }
  })
  const edges: Edge[] = definition.Graph.Relations.map(relation => ({ id: relation.ID, source: relation.FromTaskID, target: relation.ToTaskID, markerEnd: { type: MarkerType.ArrowClosed }, type: 'smoothstep' }))
  return <div className="workflow-graph"><ReactFlow nodes={nodes} edges={edges} fitView fitViewOptions={{ padding: .25 }} minZoom={.5} maxZoom={1.4} nodesDraggable={false} nodesConnectable={false} elementsSelectable proOptions={{ hideAttribution: true }} onNodeClick={(_, node) => { const latest = runtimeByDefinition.get(node.id)?.at(-1); if (latest) onSelect(latest.ID) }}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}

function RuntimeWorkflowGraph({ tasks, relations, selectedTaskID, onSelect }: { tasks: Task[]; relations: TaskRelation[]; selectedTaskID: string | null; onSelect: (id: string) => void }) {
  const nodes: Node[] = tasks.map((task, index) => ({ id: task.ID, position: { x: (task.Position ?? index) * 250, y: 0 }, data: { label: <div className="flow-node-content"><Status value={task.Status} /><strong>{task.Title}</strong></div> }, className: `flow-node status-node-${task.Status} ${selectedTaskID === task.ID ? 'selected' : ''}`, sourcePosition: Position.Right, targetPosition: Position.Left }))
  const ids = new Set(tasks.map(task => task.ID)); const edges: Edge[] = relations.filter(relation => ids.has(relation.FromTaskID) && ids.has(relation.ToTaskID)).map(relation => ({ id: `${relation.FromTaskID}-${relation.ToTaskID}`, source: relation.FromTaskID, target: relation.ToTaskID, markerEnd: { type: MarkerType.ArrowClosed }, type: 'smoothstep' }))
  return <div className="workflow-graph"><ReactFlow nodes={nodes} edges={edges} fitView nodesDraggable={false} nodesConnectable={false} proOptions={{ hideAttribution: true }} onNodeClick={(_, node) => onSelect(node.id)}><Background gap={24} size={1} /><Controls showInteractive={false} /></ReactFlow></div>
}
