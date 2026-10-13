let activeWorkspaceId = null

async function post(path, body, options = {}) {
  const response = await fetch(`/api/v1/${path}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json;charset=UTF-8',
      'X-Time-Zone': 'Asia/Shanghai',
      'X-Requested-With': 'NCode-Web',
      ...(activeWorkspaceId ? { 'X-Workspace-ID': String(activeWorkspaceId) } : {}),
      ...(options.idempotencyKey ? { 'Idempotency-Key': options.idempotencyKey } : {}),
    },
    credentials: 'same-origin',
    body: JSON.stringify(body ?? {}),
  })
  let payload
  try {
    payload = await response.json()
  } catch {
    throw new Error('服务暂时无法读取响应，请稍后重试。')
  }
  if (!response.ok || payload.code !== 200) {
    const error = new Error(payload.msg || '操作未完成，请重试。')
    error.status = response.status
    error.data = payload.data
    throw error
  }
  return payload
}

async function get(path) {
  const response = await fetch(`/api/v1/${path}`, {
    method: 'GET',
    headers: {
      'X-Requested-With': 'NCode-Web',
      ...(activeWorkspaceId ? { 'X-Workspace-ID': String(activeWorkspaceId) } : {}),
    },
    credentials: 'same-origin',
  })
  let payload
  try {
    payload = await response.json()
  } catch {
    throw new Error('服务暂时无法读取响应，请稍后重试。')
  }
  if (!response.ok || payload.code !== 200) {
    const error = new Error(payload.msg || '操作未完成，请重试。')
    error.status = response.status
    error.data = payload.data
    throw error
  }
  return payload
}

export const api = {
  setWorkspace: (workspaceId) => { activeWorkspaceId = workspaceId },
  me: () => get('auth/me'),
  register: (body) => post('auth/register', body),
  verifyEmail: (token) => post('auth/verify', { token }),
  login: (body) => post('auth/login', body),
  logout: () => post('auth/logout', {}),
  createWorkspace: (body) => post('workspace/create', body),
  createWorkspaceInvite: (body) => post('workspace/invite/create', body),
  acceptWorkspaceInvite: (token) => post('workspace/invite/accept', { token }),
  projects: (page = 1) => post('project/query/page', { page, size: 100, filters: {} }),
  project: (id) => post('project/query/one', { id }),
  createProject: (body) => post('project/create', body, { idempotencyKey: crypto.randomUUID() }),
  workItems: (projectId, page = 1) => post('workItem/query/page', { page, size: 100, filters: { projectId } }),
  createWorkItem: (body) => post('workItem/create', body, { idempotencyKey: crypto.randomUUID() }),
  updateWorkItem: (body) => post('workItem/update', body),
  transitionWorkItem: (body) => post('workItem/transition', body, { idempotencyKey: crypto.randomUUID() }),
  acceptWorkItem: (body) => post('workItem/accept', body, { idempotencyKey: crypto.randomUUID() }),
  activity: (workItemId) => post('workItem/query/activity', { workItemId }),
  repositories: (page = 1) => post('repository/query/page', { page, size: 100 }),
  createRepository: (body) => post('repository/create', body),
  repositoryRefs: (repositoryId) => post('repository/query/refs', { repositoryId }),
  repositoryCommits: (repositoryId, ref) => post('repository/query/commits', { repositoryId, ref }),
  repositoryDiff: (repositoryId, baseRef, compareRef) => post('repository/query/diff', { repositoryId, baseRef, compareRef }),
  repositoryTree: (repositoryId, path = '', ref = '') => post('repository/query/tree', { repositoryId, path, ref }),
  repositoryFile: (repositoryId, path, ref = '') => post('repository/query/file', { repositoryId, path, ref }),
}
