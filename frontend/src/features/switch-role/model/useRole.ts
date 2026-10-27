import { useSearchParams } from 'react-router-dom'

export type Role = 'guest' | 'hotelier' | 'admin'

export const DEFAULT_HOTELIER_HOTEL = 326

// Роль живёт в URL (?role=hotelier&hotel=326), чтобы демо-сценарий открывался ссылкой.
export function useRole(): { role: Role; hotelId: number } {
  const [params] = useSearchParams()
  const r = params.get('role')
  const role: Role = r === 'hotelier' || r === 'admin' ? r : 'guest'
  const hotel = Number(params.get('hotel'))
  return { role, hotelId: Number.isInteger(hotel) && hotel > 0 ? hotel : DEFAULT_HOTELIER_HOTEL }
}

export function roleHref(role: Role, hotelId = DEFAULT_HOTELIER_HOTEL): string {
  switch (role) {
    case 'hotelier':
      return `/hotelier?role=hotelier&hotel=${hotelId}`
    case 'admin':
      return '/admin?role=admin'
    default:
      return '/'
  }
}
