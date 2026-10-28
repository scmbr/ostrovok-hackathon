import { useEffect, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import type { CompareSide } from '@/shared/api'
import { ChatPanel } from '@/widgets/chat-panel'
import { CompareView } from '@/widgets/compare-view'
import { ContractPanel } from '@/widgets/contract-panel'
import { HotelResults } from '@/widgets/hotel-results'
import { ResultsMap } from '@/widgets/hotel-map'
import type { MapHotel } from '@/widgets/hotel-map'
import { ShortlistBar } from '@/widgets/shortlist-bar'
import { nearbyTypes } from '@/entities/hotel'
import { beginSearch, lastSearch, useChat } from '@/features/chat-search'
import type { SearchEntryState } from '@/features/chat-search'
import type { ContractTarget } from '@/features/book-with-wish'

export function SearchPage() {
  const chat = useChat()
  const [highlight, setHighlight] = useState<number | null>(null)
  const [target, setTarget] = useState<ContractTarget | null>(null)
  const last = lastSearch(chat)

  // Первое сообщение с главной уходит ровно один раз: ref переживает двойной эффект StrictMode,
  // а state навигации очищается, чтобы обновление страницы или возврат не повторили поиск.
  const { state, pathname, search } = useLocation()
  const navigate = useNavigate()
  const started = useRef(false)
  const intent = (state as SearchEntryState | null)?.intent
  useEffect(() => {
    if (!intent || started.current) return
    started.current = true
    navigate({ pathname, search }, { replace: true, state: null })
    void beginSearch(intent)
  }, [intent, navigate, pathname, search])

  // Отели последней выдачи для общей карты: имена берём из выдачи.
  const mapHotels = ((): MapHotel[] => {
    for (let i = chat.turns.length - 1; i >= 0; i--) {
      const t = chat.turns[i]
      if (t.kind !== 'agent' || !t.results) continue
      const r = t.results
      if (r.kind === 'similar') return (r.similar ?? []).map((h) => ({ id: h.id, name: h.name }))
      return (r.hotels ?? []).map((h) => ({ id: h.hotel_id, name: h.name }))
    }
    return []
  })()

  const bookSide = (s: CompareSide) =>
    setTarget({ hotelId: s.hotel_id, categoryId: s.category_id, categoryName: s.category, wishes: last?.query.wishes })

  return (
    <div className="mx-auto grid max-w-7xl gap-4 px-4 py-4 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
      <div className="space-y-3">
        <div className="sticky top-14 z-20">
          <ShortlistBar />
        </div>
        <ChatPanel
          renderResults={(r, unmet) => <HotelResults results={r} unmet={unmet} highlightId={highlight} onHover={setHighlight} onBook={setTarget} />}
          renderCompare={(c) => <CompareView data={c} onBook={bookSide} />}
        />
      </div>
      <aside className="hidden lg:block">
        <div className="sticky top-16 h-[calc(100vh-6rem)]">
          <ResultsMap hotels={mapHotels} highlightId={highlight} onSelect={setHighlight} poiKinds={nearbyTypes(last?.query.wishes)} />
        </div>
      </aside>
      {target && <ContractPanel target={target} onClose={() => setTarget(null)} />}
    </div>
  )
}
