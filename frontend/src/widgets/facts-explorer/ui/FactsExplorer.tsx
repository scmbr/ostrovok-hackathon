import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { ExplainedFact, HotelierHotel } from '@/shared/api'
import { Badge, ErrorNote, Spinner } from '@/shared/ui'
import { hasLabels } from '@/shared/lib'
import { FactCard, getExplainedFacts } from '@/entities/fact'
import { getHotelierHotels } from '@/entities/question'

const ENTITY_LABEL: Record<string, string> = { hotel: 'отель', facade: 'фасад', room_category: 'категория номеров' }

// «Факт со свидетельствами»: выбираем отель, видим его факты и раскрываем цепочку надёжности каждого свидетельства.
export function FactsExplorer() {
  const [hotels, setHotels] = useState<HotelierHotel[]>([])
  const [hotelId, setHotelId] = useState(156)
  const [facts, setFacts] = useState<ExplainedFact[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getHotelierHotels().then(setHotels, () => setHotels([]))
  }, [])
  useEffect(() => {
    setFacts(null)
    setError(null)
    getExplainedFacts(hotelId).then(setFacts, (e: Error) => setError(e.message))
  }, [hotelId])

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-2">
          Отель
          <select value={hotelId} onChange={(e) => setHotelId(Number(e.target.value))} className="rounded-inner border border-line bg-white px-2 py-1">
            {hotels.some((h) => h.id === hotelId) ? null : <option value={hotelId}>{hotelId}</option>}
            {hotels.map((h) => (
              <option key={h.id} value={h.id}>
                {h.id} · {h.name}
              </option>
            ))}
          </select>
        </label>
        <Link to={`/hotel/${hotelId}`} className="text-xs text-brand-link hover:underline">
          карточка гостя
        </Link>
      </div>
      {error && <ErrorNote>{error}</ErrorNote>}
      {!facts && !error && <Spinner label="Считаю разбор…" />}
      <div className="grid gap-2 md:grid-cols-2">
        {facts?.map((f) => (
          <div key={`${f.entity_type}-${f.entity_id}-${f.key}`}>
            <FactCard
              charKey={f.key}
              valueField={f.value_field}
              explain={f}
              extra={<Badge tone="gray">{`${ENTITY_LABEL[f.entity_type] ?? f.entity_type} ${f.entity_id}`}</Badge>}
              fact={{
                title: f.title,
                value: f.value,
                confidence: f.confidence,
                source: f.evidence[0]?.source ?? '',
                observed_at: f.evidence[0]?.observed_at,
                stale: f.stale,
                weak_disagreement: f.weak_disagreement,
                conflict: f.conflict,
                evidence: [],
              }}
            />
            {!hasLabels(f.key) && <p className="text-[0.6875rem] text-warn">подпись не задана: значение показано как есть</p>}
          </div>
        ))}
      </div>
    </section>
  )
}
