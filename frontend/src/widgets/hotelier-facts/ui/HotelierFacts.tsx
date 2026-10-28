import { useCallback, useEffect, useState } from 'react'
import type { Characteristic, Fact, FactEditOutcome, HotelCardData } from '@/shared/api'
import { Badge, Card, ErrorNote, Notice, Spinner } from '@/shared/ui'
import { mainValue, pct } from '@/shared/lib'
import { FactCard, FactChangeView, getCharacteristics } from '@/entities/fact'
import { getHotel } from '@/entities/hotel'
import { FactActions } from '@/features/confirm-fact'
import type { FactAddress } from '@/features/confirm-fact'

// Значения, которые отельер может назвать при исправлении: bool и enum; числа и диапазоны берёт расчёт.
function allowed(c?: Characteristic): string[] {
  if (!c) return []
  if (c.value_type === 'bool') return ['true', 'false']
  if (c.value_type === 'enum') return c.values ?? []
  return []
}

// «Что система знает об отеле»: факты отеля, фасадов и категорий с источниками; подтвердить или исправить.
export function HotelierFacts({ hotelId }: { hotelId: number }) {
  const [hotel, setHotel] = useState<HotelCardData | null>(null)
  const [registry, setRegistry] = useState<Characteristic[]>([])
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState<FactEditOutcome | null>(null)

  const load = useCallback(() => {
    getHotel(hotelId, true).then(setHotel, (e: Error) => setError(e.message))
  }, [hotelId])

  useEffect(() => {
    setHotel(null)
    setDone(null)
    setError(null)
    load()
    getCharacteristics().then(setRegistry, () => setRegistry([]))
  }, [load])

  if (error) return <ErrorNote>{error}</ErrorNote>
  if (!hotel) return <Spinner label="Загружаю факты…" />

  const char = (key: string) => registry.find((c) => c.key === key)
  const onDone = (o: FactEditOutcome) => {
    setDone(o)
    load()
  }
  const hotelFacts: Fact[] = hotel.facts.filter((f) => f.entity_type === 'hotel')

  return (
    <section className="space-y-3">
      <h2 className="text-lg font-semibold">Что система знает об отеле</h2>
      <p className="text-xs text-ink-muted">
        Подтверждение и исправление — это ваше свидетельство: итог пересчитывает движок правды и может не совпасть с вашим словом.
      </p>

      {done && (
        <Card className="border-brand">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="font-semibold">{done.action === 'confirm' ? 'Подтверждение записано' : done.recheck ? 'Факт поставлен на пересчёт' : 'Исправление записано'}</h3>
          </div>
          {done.note && <p className="text-sm text-ink">{done.note}</p>}
          {done.change && (
            <div className="mt-2">
              <FactChangeView change={done.change} trust={done.trust} />
            </div>
          )}
        </Card>
      )}
      {done?.change?.conflict_change === 'opened' && <Notice>Исправление записано; расчёт с ним не согласен — открыт конфликт.</Notice>}

      <div className="grid gap-2 md:grid-cols-2">
        {hotelFacts.map((f) => (
          <div key={`${f.entity_type}-${f.entity_id}-${f.key}`}>
            <FactCard fact={f} valueField={char(f.key)?.value_field} charKey={f.key} />
            <FactActions
              hotelId={hotelId}
              address={{ entity_type: f.entity_type, entity_id: f.entity_id, key: f.key } as FactAddress}
              values={allowed(char(f.key))}
              onDone={onDone}
            />
          </div>
        ))}
      </div>

      {hotel.rooms && hotel.rooms.facades.some((f) => f.values.length > 0) && (
        <div>
          <h3 className="mb-1 font-medium">Фасады</h3>
          <div className="grid gap-2 md:grid-cols-2">
            {hotel.rooms.facades.flatMap((fa) =>
              fa.values.map((v) => (
                <div key={`${fa.id}-${v.key}`} className="rounded-inner border border-line p-3">
                  <div className="flex flex-wrap items-center gap-2 text-sm">
                    <span className="font-medium">{char(v.key)?.title ?? v.key}</span>
                    <span>
                      {fa.side}
                      {fa.courtyard ? ', во двор' : ''}: {mainValue(v.value, char(v.key)?.value_field, v.key)}
                    </span>
                    <Badge tone={v.confidence >= 0.75 ? 'green' : v.confidence >= 0.5 ? 'amber' : 'red'}>уверенность {pct(v.confidence)}</Badge>
                  </div>
                  <FactActions
                    hotelId={hotelId}
                    address={{ entity_type: 'facade', entity_id: String(fa.id), key: v.key }}
                    values={allowed(char(v.key))}
                    onDone={onDone}
                  />
                </div>
              )),
            )}
          </div>
        </div>
      )}

      {hotel.rooms && hotel.rooms.categories.length > 0 && (
        <div>
          <h3 className="mb-1 font-medium">Категории номеров</h3>
          <div className="space-y-3">
            {hotel.rooms.categories.map((cat) => (
              <div key={cat.id} className="space-y-2">
                {cat.characteristics
                  .filter((c) => c.fact)
                  .map((c) => (
                    <div key={c.key}>
                      <FactCard
                        charKey={c.key}
                        valueField={c.key === 'sea_view' ? 'view' : undefined}
                        fact={{
                          ...(c.fact as NonNullable<typeof c.fact>),
                          title: `«${cat.name}»: ${c.title}`,
                          observed_at: c.fact?.evidence[0]?.observed_at,
                        }}
                      />
                      <FactActions
                        hotelId={hotelId}
                        address={{ entity_type: 'room_category', entity_id: String(cat.id), key: c.key }}
                        values={Object.keys(c.shares)}
                        onDone={onDone}
                      />
                    </div>
                  ))}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}
