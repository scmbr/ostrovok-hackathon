import { apiGet } from '@/shared/api'
import type { AgentRun, AgentRunStep } from '@/shared/api'

export async function getRuns(params: { agent?: string; hotelId?: number; limit?: number }): Promise<AgentRun[]> {
  const q = new URLSearchParams()
  if (params.agent) q.set('agent', params.agent)
  if (params.hotelId) q.set('hotel_id', String(params.hotelId))
  q.set('limit', String(params.limit ?? 60))
  const r = await apiGet<{ runs: AgentRun[] }>(`/admin/runs?${q}`)
  return r.runs
}

export async function getRunSteps(sessionId: string): Promise<AgentRunStep[]> {
  const r = await apiGet<{ steps: AgentRunStep[] }>(`/admin/runs/${encodeURIComponent(sessionId)}`)
  return r.steps
}
