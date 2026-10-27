import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Category, Characteristic, Desire, HotelCardData } from '@/shared/api'
import { Badge, Button, Card } from '@/shared/ui'
import { pct } from '@/shared/lib'
import { FactCard, getCharacteristics } from '@/entities/fact'
import { compareCategoriesHref } from '@/features/compare-hotels'
import type { ContractTarget } from '@/features/book-with-wish'

interface Props {
  hotel: HotelCardData
  wishes?: Desire[]
  onBook: (t: ContractTarget) => void
}

function CategoryBlock({ cat, selected, onToggle, onBook }: { cat: Category; selected: boolean; onToggle: () => void; onBook: () => void }) {
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-semibold">{cat.name}</h3>
        <span className="text-xs text-slate-500">{cat.rooms_count} номеров</span>
        <label className="ml-auto flex items-center gap-1 text-xs text-slate-700">
          <input type="checkbox" checked={selected} onChange={onToggle} /> в сравнение
        </label>
        <Button small variant="primary" onClick={onBook}>
          Контракт и бронь
        </Button>
      </div>
      <div className="mt-3 space-y-2">
        {cat.characteristics.map((c) => (
          <div key={c.key} className="rounded-md bg-slate-50 p-2 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{c.title}</span>
              <span>
                {c.target}: {pct(c.probability)} номеров
              </span>
              {c.guarantee ? <Badge tone="green">гарантия</Badge> : <Badge tone="blue">шанс {pct(c.probability)}</Badge>}
              {c.unknown > 0 && <Badge tone="gray">данных нет у {pct(c.unknown)}</Badge>}
            </div>
            {c.wish && (
              <p className="mt-0.5 text-xs text-slate-700">
                Пожелание к брони: <b>{c.wish.text}</b> ({c.wish.rooms.toFixed(1)} номеров, {pct(c.wish.share)})
              </p>
            )}
            {c.fact && (
              <div className="mt-2">
                <FactCard
                  fact={{ ...c.fact, title: `Итог по категории: ${c.title}`, observed_at: c.fact.evidence[0]?.observed_at }}
                />
              </div>
            )}
          </div>
        ))}
      </div>
    </Card>
  )
}

// Страница отеля: факты с «откуда известно» и категории номеров с вероятностью и гарантией.
export function HotelFacts({ hotel, wishes, onBook }: Props) {
  const [registry, setRegistry] = useState<Characteristic[]>([])
  const [picked, setPicked] = useState<number[]>([])
  useEffect(() => {
    getCharacteristics().then(setRegistry, () => setRegistry([]))
  }, [])
  const field = (key: string) => registry.find((c) => c.key === key)?.value_field

  const toggle = (id: number) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))
  const bookTarget = (cat: Category): ContractTarget => {
    const w = cat.characteristics.find((c) => c.wish)?.wish
    return {
      hotelId: hotel.id,
      categoryId: cat.id,
      categoryName: cat.name,
      wishes,
      offer: w ? { facadeId: w.facade_id, floorFrom: w.min_floor, text: w.text } : undefined,
    }
  }

  return (
    <div className="space-y-6">
      <section>
        <h2 className="mb-2 text-lg font-semibold">Что система знает об отеле</h2>
        <p className="mb-3 text-xs text-slate-500">
          Каждое значение посчитано по открытым данным или со слов отельера и гостей; ниже — источники, их вклад и срок годности.
        </p>
        <div className="grid gap-2 md:grid-cols-2">
          {hotel.facts.map((f) => (
            <FactCard key={f.id} fact={f} valueField={field(f.key)} />
          ))}
        </div>
      </section>

      {hotel.rooms && (
        <section>
          <div className="mb-2 flex flex-wrap items-center gap-3">
            <h2 className="text-lg font-semibold">Категории номеров</h2>
            {picked.length >= 2 && (
              <Link
                to={compareCategoriesHref(hotel.id, picked)}
                className="rounded-md bg-sky-700 px-3 py-1.5 text-sm font-medium text-white hover:bg-sky-800"
              >
                Сравнить выбранные ({picked.length})
              </Link>
            )}
            {picked.length < 2 && <span className="text-xs text-slate-500">Отметьте две категории, чтобы сравнить</span>}
          </div>
          <div className="space-y-3">
            {hotel.rooms.categories.map((c) => (
              <CategoryBlock
                key={c.id}
                cat={c}
                selected={picked.includes(c.id)}
                onToggle={() => toggle(c.id)}
                onBook={() => onBook(bookTarget(c))}
              />
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
