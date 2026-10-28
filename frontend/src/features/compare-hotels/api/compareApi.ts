import { apiPost } from '@/shared/api'
import type { Compare, Desire } from '@/shared/api'
import { touchSession, withSession } from '@/entities/session'

export interface CompareParams {
  // Желания гостя из последней выдачи; без них сервер берёт желания сессии.
  wishes?: Desire[]
  hotelIds?: number[]
  // Сравнение категорий одного отеля.
  hotelId?: number
  categoryIds?: number[]
}

export function compare(p: CompareParams): Promise<Compare> {
  touchSession()
  return withSession((sid) =>
    apiPost<Compare>('/compare', {
    session_id: sid,
    ...(p.wishes?.length ? { wishes: p.wishes } : {}),
    ...(p.hotelIds ? { hotel_ids: p.hotelIds } : {}),
    ...(p.hotelId ? { hotel_id: p.hotelId, category_ids: p.categoryIds } : {}),
  }),
  )
}

export const compareHref = (hotelIds: number[]) => `/compare?hotels=${hotelIds.join(',')}`
export const compareCategoriesHref = (hotelId: number, categoryIds: number[]) => `/compare?hotel=${hotelId}&cats=${categoryIds.join(',')}`
