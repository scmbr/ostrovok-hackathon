import { useEffect } from 'react'
import { addDays, nightsWord, toIso } from '@/shared/lib'
import { useStayDates } from '../model/useStayDates'

// Поле вложено в контейнер с радиусом 16, поэтому его радиус меньше (8), как у малых кнопок.
const field = 'h-8 rounded-sm bg-surface-muted px-2 text-caption text-ink outline-none focus:ring-2 focus:ring-brand'

export function StayDatesPicker() {
  const { stay, inUrl, set, setCheckin, setCheckout } = useStayDates()

  // Ссылка без дат дополняется текущими, чтобы её можно было отправить дальше.
  useEffect(() => {
    if (!inUrl) set(stay.checkin, stay.checkout)
  }, [inUrl, stay.checkin, stay.checkout, set])

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-ink-muted">
      <label className="flex items-center gap-1.5">
        Заезд
        <input
          type="date"
          className={field}
          value={stay.checkin}
          min={toIso(new Date())}
          onChange={(e) => e.target.value && setCheckin(e.target.value)}
        />
      </label>
      <label className="flex items-center gap-1.5">
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
      <span className="text-[0.6875rem] leading-4">наличие мест не проверяется — прототип</span>
    </div>
  )
}
