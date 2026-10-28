import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { addDays, defaultStay, validStay } from '@/shared/lib'
import type { Stay } from '@/shared/lib'

const STORAGE_KEY = 'stay_dates'

function readStored(): Stay | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    const v = raw ? (JSON.parse(raw) as { checkin?: string; checkout?: string }) : null
    return v ? validStay(v.checkin, v.checkout) : null
  } catch {
    return null
  }
}

function writeStored(s: Stay) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ checkin: s.checkin, checkout: s.checkout }))
  } catch {
    // без хранилища даты живут только в URL
  }
}

// Даты проживания живут в URL (?checkin=&checkout=). Если в ссылке их нет (например, переход по внутренней ссылке),
// берутся последние выбранные, иначе умолчание: через 14 дней, 3 ночи. Наличие мест не проверяется.
export function useStayDates() {
  const [params, setParams] = useSearchParams()
  const fromUrl = validStay(params.get('checkin'), params.get('checkout'))
  const fallback = useMemo(() => readStored() ?? defaultStay(), [])
  const stay = fromUrl ?? fallback

  const set = useCallback(
    (checkin: string, checkout: string) => {
      const next = validStay(checkin, checkout)
      if (!next) return
      writeStored(next)
      setParams(
        (p) => {
          const q = new URLSearchParams(p)
          q.set('checkin', next.checkin)
          q.set('checkout', next.checkout)
          return q
        },
        { replace: true },
      )
    },
    [setParams],
  )

  // Сдвиг заезда сохраняет число ночей, если выезд оказался бы не позже заезда.
  const setCheckin = (checkin: string) => set(checkin, stay.checkout > checkin ? stay.checkout : addDays(checkin, stay.nights))
  const setCheckout = (checkout: string) => set(stay.checkin, checkout)

  return { stay, inUrl: fromUrl !== null, set, setCheckin, setCheckout }
}
