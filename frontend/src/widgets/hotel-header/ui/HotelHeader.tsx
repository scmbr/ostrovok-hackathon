import { Link } from 'react-router-dom'
import type { Desire, HotelCardData } from '@/shared/api'
import { Button } from '@/shared/ui'
import { districtLabel } from '@/shared/lib'
import { pickCategory } from '@/entities/hotel'
import { makeTarget } from '@/features/book-with-wish'
import type { ContractTarget } from '@/features/book-with-wish'
import { FindSimilarButton } from '@/features/find-similar'
import { ShortlistButton } from '@/features/shortlist'

interface Props {
  hotel: HotelCardData
  wishes?: Desire[]
  // Категория, которую поиск уже выбрал лучшей для этого гостя.
  bestCategoryId?: number
  onBook: (t: ContractTarget) => void
}

// Шапка страницы отеля: название, звёзды, примерный адрес, район, цена «от» и главные действия.
export function HotelHeader({ hotel, wishes, bestCategoryId, onBook }: Props) {
  const categories = hotel.rooms?.categories ?? []
  const cat = pickCategory(categories, (wishes ?? []).map((w) => w.key), bestCategoryId)
  const prices = categories.map((c) => c.price_eur).filter((p): p is number => !!p)
  const from = prices.length ? Math.min(...prices) : hotel.card.price_eur
  const place = [hotel.address.street, districtLabel(hotel.district)].filter(Boolean).join(', ')

  return (
    <div className="flex flex-wrap items-start gap-3">
      <div className="min-w-0">
        <Link to="/search" className="text-xs text-brand-link hover:underline">
          ← к поиску
        </Link>
        <h1 className="text-2xl font-bold">
          {hotel.name} <span className="text-base font-normal text-warn">{'★'.repeat(hotel.card.stars)}</span>
        </h1>
        <p className="text-sm text-ink" title={hotel.address.approx ? 'ближайшая улица по OSM, точного адреса нет' : undefined}>
          {hotel.address.approx && '≈ '}
          {place}
        </p>
        <p className="mt-1 text-ink" title={hotel.card.mock ? 'Цена и звёзды — мок: базовая карточка Островка заменена генератором' : undefined}>
          <span className="text-caption text-ink-muted">от </span>
          <span className="text-price">{from} €</span>
          <span className="ml-1 text-caption text-ink-muted">за ночь{hotel.card.mock && ' · мок'}</span>
        </p>
      </div>
      <div className="ml-auto flex flex-wrap gap-2">
        <ShortlistButton hotelId={hotel.id} />
        <FindSimilarButton hotelId={hotel.id} hotelName={hotel.name} />
        {cat && (
          <Button small variant="primary" onClick={() => onBook(makeTarget(hotel.id, cat, wishes))} title={`Контракт и бронь: ${cat.name}`}>
            Забронировать
          </Button>
        )}
      </div>
    </div>
  )
}
