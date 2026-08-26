export type HomeView = 'all' | 'human'
export type RouteState = {
  workItemID: string | null
  taskID: string | null
  homeView: HomeView
  blackboardID?: string | null
  blackboardVersion?: number | null
  workflowID?: string | null
  workflowVersion?: number | null
  workflowEditing?: boolean
}

export function readRoute(pathname: string): RouteState {
  let parts: string[]
  try {
    parts = pathname.split('/').filter(Boolean).map(part => decodeURIComponent(part))
  } catch {
    return { workItemID: null, taskID: null, homeView: 'all' }
  }
  if (parts[0] === 'blackboards') {
    const version = parts[2] === 'versions' ? Number(parts[3]) : null
    return { workItemID: null, taskID: null, homeView: 'all', blackboardID: parts[1] ?? null, blackboardVersion: Number.isInteger(version) && version! > 0 ? version : null }
  }
  if (parts[0] === 'workflows') {
    if (parts[1] === 'new') return { workItemID: null, taskID: null, homeView: 'all', workflowID: null, workflowVersion: null, workflowEditing: true }
    const version = parts[2] === 'versions' ? Number(parts[3]) : null
    return { workItemID: null, taskID: null, homeView: 'all', workflowID: parts[1] ?? null, workflowVersion: Number.isInteger(version) && version! > 0 ? version : null, ...(parts[4] === 'edit' ? { workflowEditing: true } : {}) }
  }
  if (parts[0] === 'work-items' && parts[1]) {
    return { workItemID: parts[1], taskID: parts[2] === 'tasks' && parts[3] ? parts[3] : null, homeView: 'all' }
  }
  return { workItemID: null, taskID: null, homeView: parts[0] === 'attention' ? 'human' : 'all' }
}

export function routePath(route: RouteState) {
  if (route.blackboardID !== undefined) {
    const base = route.blackboardID ? `/blackboards/${encodeURIComponent(route.blackboardID)}` : '/blackboards'
    return route.blackboardID && route.blackboardVersion ? `${base}/versions/${route.blackboardVersion}` : base
  }
  if (route.workflowID !== undefined) {
    if (route.workflowEditing && !route.workflowID) return '/workflows/new'
    const base = route.workflowID ? `/workflows/${encodeURIComponent(route.workflowID)}` : '/workflows'
    const versionPath = route.workflowID && route.workflowVersion ? `${base}/versions/${route.workflowVersion}` : base
    return route.workflowEditing ? `${versionPath}/edit` : versionPath
  }
  if (!route.workItemID) return route.homeView === 'human' ? '/attention' : '/'
  const workPath = `/work-items/${encodeURIComponent(route.workItemID)}`
  return route.taskID ? `${workPath}/tasks/${encodeURIComponent(route.taskID)}` : workPath
}
