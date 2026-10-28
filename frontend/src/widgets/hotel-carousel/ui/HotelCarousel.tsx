import { useEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import type { ShowcaseHotel } from '@/shared/api'
import { Icon } from '@/shared/ui'
import { HotelTeaser, hasHotelPhoto, listDemoHotels } from '@/entities/hotel'

// Карусель демо-отелей: горизонтальный scroll-snap и стрелки. Нет данных или ошибка — блока нет вовсе.
export function HotelCarousel() {
  const [hotels, setHotels] = useState<ShowcaseHotel[]>([])
  const track = useRef<HTMLUListElement>(null)
  // Даты проживания едут в карточку отеля вместе со ссылкой.
  const { search } = useLocation()
  useEffect(() => {
    // Сначала отели с фото; сортировка устойчивая, порядок внутри групп остаётся прежним.
    listDemoHotels().then((hs) => setHotels([...hs].sort((a, b) => +hasHotelPhoto(b.id) - +hasHotelPhoto(a.id))), () => setHotels([]))
  }, [])
  if (!hotels.length) return null

  const scroll = (dir: 1 | -1) => {
    const el = track.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: 'smooth' })
  }
  const arrow =
    'hidden h-8 w-8 shrink-0 items-center justify-center rounded-sm bg-surface-muted text-ink hover:bg-brand-tint md:flex'

  return (
    <section className="mx-auto max-w-7xl px-4 pb-16 pt-12" aria-label="Отели в Барселоне">
      <div className="mb-5 flex items-center justify-between">
        <h2 className="font-display text-2xl font-bold text-ink">Отели в Барселоне</h2>
        <div className="flex gap-2">
          <button type="button" className={arrow} onClick={() => scroll(-1)} aria-label="Назад">
            <Icon name="chevron-left" />
          </button>
          <button type="button" className={arrow} onClick={() => scroll(1)} aria-label="Вперёд">
            <Icon name="chevron-right" />
          </button>
        </div>
      </div>
      <ul
        ref={track}
        className="flex snap-x snap-mandatory gap-3 overflow-x-auto pb-2 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      >
        {hotels.map((h) => (
          <li key={h.id} className="w-80 shrink-0 snap-start">
            <Link to={{ pathname: `/hotel/${h.id}`, search }} className="block h-full">
              <HotelTeaser hotel={h} />
            </Link>
          </li>
        ))}
      </ul>
    </section>
  )
}
