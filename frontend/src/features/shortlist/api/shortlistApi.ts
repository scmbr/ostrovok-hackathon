import { apiDelete, apiGet, apiPost } from '@/shared/api'
import type { Shown } from '@/shared/api'

interface ShortlistResponse {
  session_id: string
  hotels: Shown[]
}

export const fetchShortlist = (session: string) => apiGet<ShortlistResponse>(`/sessions/${encodeURIComponent(session)}/shortlist`)

export const addToShortlist = (session: string, hotelId: number) =>
  apiPost<ShortlistResponse>(`/sessions/${encodeURIComponent(session)}/shortlist/${hotelId}`, {})

export const removeFromShortlist = (session: string, hotelId: number) =>
  apiDelete<ShortlistResponse>(`/sessions/${encodeURIComponent(session)}/shortlist/${hotelId}`)
