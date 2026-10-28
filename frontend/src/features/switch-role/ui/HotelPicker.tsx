import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { HotelierHotel } from '@/shared/api'
import { personaLabel } from '@/shared/lib'
import { getHotelierHotels } from '@/entities/question'
import { roleHref } from '../model/useRole'

// Отели демо-сценариев идут первыми: 326 приукрашивает, 156 — гости против расчёта, 161 — бронь с пожеланием и т. д.
export const SCENARIO_HOTELS = [326, 156, 161, 188, 343, 177, 57, 143]

const label = (h: HotelierHotel) => `${h.id} · ${h.name}${h.persona ? ` — имитация: ${personaLabel(h.persona)}` : ''}`

// Выбор отеля для роли отельера: сначала сценарии, остальные демо-отели — поиском по названию.
export function HotelPicker({ hotelId }: { hotelId: number }) {
  const navigate = useNavigate()
  const [hotels, setHotels] = useState<HotelierHotel[]>([])
  const [query, setQuery] = useState('')
  useEffect(() => {
    getHotelierHotels().then(setHotels, () => setHotels([]))
  }, [])

  const go = (id: number) => navigate(roleHref('hotelier', id))
  const scenarios = useMemo(
    () => SCENARIO_HOTELS.map((id) => hotels.find((h) => h.id === id)).filter((h): h is HotelierHotel => !!h),
    [hotels],
  )
  const found = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return []
    return hotels.filter((h) => !SCENARIO_HOTELS.includes(h.id) && (h.name.toLowerCase().includes(q) || String(h.id) === q)).slice(0, 8)
  }, [hotels, query])

  return (
    <div className="flex flex-wrap items-start gap-3 text-sm">
      <label className="flex items-center gap-2">
        <span className="text-ink">Отель:</span>
        <select
          value={SCENARIO_HOTELS.includes(hotelId) ? hotelId : ''}
          onChange={(e) => e.target.value && go(Number(e.target.value))}
          className="rounded-inner border border-line bg-white px-2 py-1"
        >
          {!SCENARIO_HOTELS.includes(hotelId) && <option value="">{hotels.find((h) => h.id === hotelId)?.name ?? `отель ${hotelId}`}</option>}
          {scenarios.map((h) => (
            <option key={h.id} value={h.id}>
              {label(h)}
            </option>
          ))}
        </select>
      </label>
      <div className="relative">
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Другой демо-отель: название или номер"
          className="w-72 rounded-inner border border-line px-2 py-1"
        />
        {found.length > 0 && (
          <ul className="absolute z-10 mt-1 max-h-64 w-full overflow-y-auto rounded-inner border border-line bg-white ">
            {found.map((h) => (
              <li key={h.id}>
                <button
                  type="button"
                  className="block w-full px-2 py-1 text-left hover:bg-brand-tint"
                  onClick={() => {
                    setQuery('')
                    go(h.id)
                  }}
                >
                  {label(h)}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
