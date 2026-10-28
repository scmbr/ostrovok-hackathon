import { useEffect } from 'react'
import { addDays, nightsWord, toIso } from '@/shared/lib'
import { useStayDates } from '../model/useStayDates'

const field = 'h-7 rounded-inner bg-surface-muted px-2 text-caption text-ink outline-none focus:ring-2 focus:ring-brand'

export function StayDatesPicker() {
  const { stay, inUrl, set, setCheckin, setCheckout } = useStayDates()

  // Ссылка без дат дополняется текущими, чтобы её можно было отправить дальше.
  useEffect(() => {
    if (!inUrl) set(stay.checkin, stay.checkout)
  }, [inUrl, stay.checkin, stay.checkout, set])

  return (
    <div className="hidden flex-col items-end md:flex">
      <div className="flex items-center gap-1.5 text-caption text-ink-muted">
        <label className="flex items-center gap-1">
          Заезд
          <input
            type="date"
            className={field}
            value={stay.checkin}
            min={toIso(new Date())}
            onChange={(e) => e.target.value && setCheckin(e.target.value)}
          />
        </label>
        <label className="flex items-center gap-1">
          Выезд
          <input
            type="date"
            className={field}
            value={stay.checkout}
            min={addDays(stay.checkin, 1)}
            onChange={(e) => e.target.value && setCheckout(e.target.value)}
          />
        </label>
        <span className="font-medium text-ink">
          {stay.nights} {nightsWord(stay.nights)}
        </span>
      </div>
      <span className="text-[11px] leading-3 text-ink-muted">наличие мест не проверяется — прототип</span>
    </div>
  )
}
