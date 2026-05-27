const BASE_URL = '/api'

export async function fetchAPI(path, params = {}) {
  const url = new URL(BASE_URL + path, window.location.origin)
  Object.entries(params).forEach(([key, val]) => {
    if (val !== undefined && val !== null && val !== '') {
      url.searchParams.set(key, val)
    }
  })
  const res = await fetch(url.toString())
  if (!res.ok) throw new Error(`API error: ${res.status}`)
  return res.json()
}

export const api = {
  overview:      ()        => fetchAPI('/overview'),
  memory:        (params)  => fetchAPI('/memory', params),
  memoryDetail:  (id)      => fetchAPI(`/memory/${id}`),
  skills:        (params)  => fetchAPI('/skills', params),
  skillDetail:   (id)      => fetchAPI(`/skills/${id}`),
  skillLineage:  (id)      => fetchAPI(`/skills/${id}/lineage`),
  compression:   (params)  => fetchAPI('/compression/samples', params),
  routing:       (params)  => fetchAPI('/routing', params),
  routingDetail: (taskId)  => fetchAPI(`/routing/${taskId}`),
  plans:         (params)  => fetchAPI('/plans', params),
  planDetail:    (id)      => fetchAPI(`/plans/${id}`),
  planStats:     ()        => fetchAPI('/plans/stats'),
  graphNodes:    ()        => fetchAPI('/graph/nodes'),
  graphEdges:    ()        => fetchAPI('/graph/edges'),
  graphNode:     (id)      => fetchAPI(`/graph/node/${id}`),
  costSummary:   (params)  => fetchAPI('/cost-summary', params),
}
