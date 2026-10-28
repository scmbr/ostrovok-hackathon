import { Link } from 'react-router-dom'
import type { Group, ResultsEvent, SearchHotel, Variant } from '@/shared/api'
import { Button } from '@/shared/ui'
import { HotelResultCard, SimilarList } from '@/entities/hotel'
import { ShortlistButton } from '@/features/shortlist'
import { FindSimilarButton } from '@/features/find-similar'
import { compareCategoriesHref } from '@/features/compare-hotels'
import { useStayDates } from '@/features/stay-dates'
import type { ContractTarget } from '@/features/book-with-wish'

interface Props {
  results: ResultsEvent
  // Желания, которые система не умеет проверять: выдача считается только по остальным.
  unmet?: string[]
  highlightId?: number | null
  onHover?: (id: number | null) => void
  onBook: (t: ContractTarget) => void
}

const KIND_HINT: Record<string, string> = {
  full: 'Подходят по всем желаниям',
  open: 'Ничего не нарушено, но часть данных под вопросом',
}

const CHECKABLE = 'желаниям, которые мы умеем проверять'

// Если часть пожеланий не проверяется, заголовок «по всем желаниям» был бы неправдой: оговариваем, по чему именно.
function groupHeading(g: Group, hasUnmet: boolean): string | null {
  if (!hasUnmet) return g.title || KIND_HINT[g.kind] || null
  if (g.kind === 'full') return 'Подходят по желаниям, которые мы умеем проверять'
  if (g.kind === 'open') return `По ${CHECKABLE} ничего не нарушено, но часть данных под вопросом`
  if (g.title) return `${g.title} (среди проверяемых желаний)`
  return null
}

function targetOf(hotel: SearchHotel, v: Variant, results: ResultsEvent): ContractTarget {
  const bw = v.wishes.find((w) => w.booking_wish)?.booking_wish
  return {
    hotelId: hotel.hotel_id,
    categoryId: v.category_id,
    categoryName: v.name,
    wishes: results.query.wishes,
    offer: bw ? { facadeId: bw.facade_id, floorFrom: bw.min_floor, text: bw.text, probability: bw.probability } : undefined,
  }
}

// Выдача по группам: «полностью», «под вопросом», блоки «всё, кроме X». Тексты объяснений приходят с сервера готовыми.
export function HotelResults({ results, unmet = [], highlightId, onHover, onBook }: Props) {
  const { stay } = useStayDates()
  if (results.kind === 'similar') {
    return (
      <div className="space-y-2">
        <p className="text-sm font-medium">Похожие на «{results.base?.name}»</p>
        <SimilarList hotels={results.similar ?? []} actions={(h) => <ShortlistButton hotelId={h.id} />} />
      </div>
    )
  }
  const groups: Group[] = results.groups?.length
    ? results.groups
    : [{ kind: 'ranked', hotels: results.hotels ?? [], explanations: results.explanations ?? [] }]
  if (!groups.some((g) => g.hotels.length)) {
    return <p className="text-sm text-ink">Ничего не нашлось. Попробуйте смягчить пожелания.</p>
  }
  return (
    <div className="space-y-4">
      {results.groups_lead && (
        <p className="text-sm text-ink">{unmet.length ? results.groups_lead.replace('по всем желаниям', `по ${CHECKABLE}`) : results.groups_lead}</p>
      )}
      {unmet.length > 0 && (
        <p className="rounded-inner bg-warn-bg px-3 py-2 text-xs text-warn">Не проверяем: {unmet.map((u) => `«${u}»`).join(', ')}</p>
      )}
      {groups.map((g, gi) => (
        <section key={gi} className="space-y-2">
          {groupHeading(g, unmet.length > 0) && (
            <h3 className="text-sm font-semibold text-ink">{groupHeading(g, unmet.length > 0)}</h3>
          )}
          {g.hotels.map((h) => (
            <HotelResultCard
              key={h.hotel_id}
              hotel={h}
              explanation={g.explanations.find((x) => x.hotel_id === h.hotel_id)}
              highlighted={highlightId === h.hotel_id}
              nights={stay.nights}
              onHover={onHover}
              actions={
                <>
                  <ShortlistButton hotelId={h.hotel_id} />
                  <FindSimilarButton hotelId={h.hotel_id} hotelName={h.name} />
                  <Link
                    to={`/hotel/${h.hotel_id}`}
                    className="inline-flex items-center rounded-inner px-2 py-1 text-xs font-medium text-brand-link ring-1 ring-inset ring-line hover:bg-brand-tint"
                  >
                    Подробнее
                  </Link>
                </>
              }
              variantActions={(v) => (
                <>
                  {v === h.best && h.alternative && (
                    <Link
                      to={compareCategoriesHref(h.hotel_id, [h.best.category_id, h.alternative.category_id])}
                      className="inline-flex items-center rounded-inner px-2 py-1 text-xs font-medium text-brand-link hover:bg-brand-tint"
                    >
                      Сравнить с альтернативой
                    </Link>
                  )}
                  <Button small variant="primary" onClick={() => onBook(targetOf(h, v, results))}>
                    Контракт и бронь
                  </Button>
                </>
              )}
            />
          ))}
        </section>
      ))}
    </div>
  )
}
