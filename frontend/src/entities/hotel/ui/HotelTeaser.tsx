import type { ShowcaseHotel } from '@/shared/api'
import { districtLabel } from '@/shared/lib'
import { Badge, Icon } from '@/shared/ui'

// Короткая карточка для карусели на главной: без фото (плашка с иконкой), только район, цена и проверенные факты.
export function HotelTeaser({ hotel }: { hotel: ShowcaseHotel }) {
  return (
    <div className="flex h-full flex-col rounded-card bg-surface p-1 ring-1 ring-line transition-colors hover:ring-brand">
      <div className="flex h-24 items-center justify-center rounded-inner bg-brand-soft text-brand-link">
        <Icon name="hotel" size={32} />
      </div>
      <div className="flex flex-1 flex-col gap-1 p-2">
        <h3 className="line-clamp-2 text-title text-ink">{hotel.name}</h3>
        <p className="text-caption text-ink">
          {districtLabel(hotel.district)}
          {hotel.stars > 0 && <span className="ml-1 text-ink-muted">{'★'.repeat(hotel.stars)}</span>}
        </p>
        {(hotel.sea_view || hotel.walk_to_beach_min > 0) && (
          <div className="flex flex-wrap gap-1">
            {hotel.sea_view && <Badge tone="blue">Вид на море</Badge>}
            {hotel.walk_to_beach_min > 0 && <Badge tone="gray">До пляжа {hotel.walk_to_beach_min} мин пешком</Badge>}
          </div>
        )}
        <p className="mt-auto pt-1 text-ink" title={hotel.mock ? 'Цена — мок: базовая карточка Островка заменена генератором' : undefined}>
          <span className="text-caption text-ink-muted">от </span>
          <span className="text-price">{hotel.price_eur} €</span>
          <span className="ml-1 text-caption text-ink-muted">за ночь</span>
        </p>
      </div>
    </div>
  )
}
