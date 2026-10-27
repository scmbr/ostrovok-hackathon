import { apiGet } from '@/shared/api'
import type { AdminConflict, RecomputeTask, StaleFactRow } from '@/shared/api'

export interface ConflictBoard {
  conflicts: AdminConflict[]
  tasks: RecomputeTask[]
  stale: StaleFactRow[]
}

export function getConflicts(status: 'open' | 'closed' | 'all', hotelId?: number): Promise<ConflictBoard> {
  const q = new URLSearchParams({ status })
  if (hotelId) q.set('hotel_id', String(hotelId))
  return apiGet<ConflictBoard>(`/admin/conflicts?${q}`)
}
