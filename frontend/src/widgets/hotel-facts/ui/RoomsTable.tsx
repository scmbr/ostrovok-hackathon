import { Fragment, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Category, CategoryChar, Desire, HotelCardData } from '@/shared/api'
import { Badge, Button } from '@/shared/ui'
import { pct } from '@/shared/lib'
import { FactCard } from '@/entities/fact'
import { compareCategoriesHref } from '@/features/compare-hotels'
import { makeTarget } from '@/features/book-with-wish'
import type { ContractTarget } from '@/features/book-with-wish'

// Без поиска показываем единственные характеристики уровня категории.
const DEFAULT_KEYS = ['sea_view', 'noise_night']

function CharCell({ ch }: { ch?: CategoryChar }) {
  if (!ch) return <span className="text-ink-muted">—</span>
  if (ch.guarantee) return <Badge tone="green">гарантия</Badge>
  if (ch.probability === 0 && ch.unknown > 0) return <Badge tone="gray">данных нет</Badge>
  return <Badge tone="blue">~{pct(ch.probability)}</Badge>
}

function Details({ hotel, cat }: { hotel: HotelCardData; cat: Category }) {
  const facades = hotel.rooms?.facades ?? []
  return (
    <div className="space-y-2 text-sm">
      <div>
        <div className="mb-1 text-xs font-medium text-ink-muted">Размещение по фасадам и этажам</div>
        <ul className="space-y-0.5 text-xs text-ink">
          {cat.placements.map((p) => {
            const f = facades.find((x) => x.id === p.facade_id)
            return (
              <li key={`${p.facade_id}-${p.floor_from}`}>
                {f ? `${f.side}${f.courtyard ? ', двор' : f.faces_street ? ', улица' : ''}` : `фасад ${p.facade_id}`}: этажи {p.floor_from}–{p.floor_to}, {p.rooms_count} номеров
              </li>
            )
          })}
        </ul>
      </div>
      {cat.characteristics.map((c) => (
        <div key={c.key} className="rounded-inner bg-surface-muted p-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{c.title}</span>
            <span>
              {c.target}: {pct(c.probability)} номеров
            </span>
            {c.unknown > 0 && <Badge tone="gray">данных нет у {pct(c.unknown)}</Badge>}
          </div>
          {c.wish && (
            <p className="mt-0.5 text-xs text-ink">
              Пожелание к брони: <b>{c.wish.text}</b> ({c.wish.rooms.toFixed(1)} номеров, {pct(c.wish.share)})
            </p>
          )}
          {c.fact && (
            <div className="mt-2">
              <FactCard charKey={c.key} fact={{ ...c.fact, title: `Итог по категории: ${c.title}`, observed_at: c.fact.evidence[0]?.observed_at }} />
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

// Компактный список категорий: название, цена, вероятность или гарантия по желаниям гостя. Размещение — по раскрытию.
export function RoomsTable({ hotel, wishes, onBook }: { hotel: HotelCardData; wishes?: Desire[]; onBook: (t: ContractTarget) => void }) {
  const [openId, setOpenId] = useState<number | null>(null)
  const [picked, setPicked] = useState<number[]>([])
  const cats = hotel.rooms?.categories ?? []
  if (!cats.length) return null

  const present = new Set(cats.flatMap((c) => c.characteristics.map((x) => x.key)))
  const wishKeys = (wishes ?? []).map((w) => w.key).filter((k) => present.has(k))
  const keys = wishKeys.length ? wishKeys : DEFAULT_KEYS.filter((k) => present.has(k))
  const titleOf = (k: string) => cats.flatMap((c) => c.characteristics).find((x) => x.key === k)?.title ?? k
  const toggle = (id: number) => setPicked((p) => (p.includes(id) ? p.filter((x) => x !== id) : [...p, id]))

  return (
    <section>
      <div className="mb-2 flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-semibold">Номера</h2>
        {picked.length >= 2 ? (
          <Link to={compareCategoriesHref(hotel.id, picked)} className="rounded-inner bg-brand px-3 py-1.5 text-sm font-medium text-white hover:opacity-90">
            Сравнить выбранные ({picked.length})
          </Link>
        ) : (
          <span className="text-xs text-ink-muted">Отметьте две категории, чтобы сравнить</span>
        )}
      </div>
      <div className="overflow-x-auto rounded-card bg-surface">
        <table className="w-full text-left text-sm">
          <thead className="text-xs text-ink-muted">
            <tr className="border-b border-line">
              <th className="p-2 font-medium" />
              <th className="p-2 font-medium">Категория</th>
              <th className="p-2 font-medium">Цена</th>
              {keys.map((k) => (
                <th key={k} className="p-2 font-medium">
                  {titleOf(k)}
                </th>
              ))}
              <th className="p-2" />
            </tr>
          </thead>
          <tbody>
            {cats.map((c) => (
              <Fragment key={c.id}>
                <tr className="border-b border-line last:border-0">
                  <td className="p-2">
                    <input type="checkbox" aria-label={`В сравнение: ${c.name}`} checked={picked.includes(c.id)} onChange={() => toggle(c.id)} />
                  </td>
                  <td className="p-2">
                    <span className="font-medium">{c.name}</span> <span className="text-xs text-ink-muted">{c.rooms_count} номеров</span>
                    {c.characteristics.some((x) => x.fact?.conflict) && <div className="text-xs font-medium text-bad">источники спорят</div>}
                    {c.characteristics.some((x) => x.fact?.trust_note) && <div className="text-xs text-warn">доверие к отелю снижено</div>}
                  </td>
                  <td className="whitespace-nowrap p-2">{c.price_eur ? `${c.price_eur} €` : '—'}</td>
                  {keys.map((k) => (
                    <td key={k} className="p-2">
                      <CharCell ch={c.characteristics.find((x) => x.key === k)} />
                    </td>
                  ))}
                  <td className="whitespace-nowrap p-2 text-right">
                    <button type="button" className="mr-2 text-xs text-brand-link hover:underline" aria-expanded={openId === c.id} onClick={() => setOpenId(openId === c.id ? null : c.id)}>
                      {openId === c.id ? 'скрыть' : 'подробнее'}
                    </button>
                    <Button small variant="primary" onClick={() => onBook(makeTarget(hotel.id, c, wishes))}>
                      Бронь
                    </Button>
                  </td>
                </tr>
                {openId === c.id && (
                  <tr className="border-b border-line">
                    <td colSpan={keys.length + 4} className="p-3">
                      <Details hotel={hotel} cat={c} />
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}
