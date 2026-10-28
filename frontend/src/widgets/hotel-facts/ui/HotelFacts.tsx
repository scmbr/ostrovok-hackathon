import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { Characteristic, Desire, Fact, HotelCardData, WishEval, WishStatus } from '@/shared/api'
import { FactRow, getCharacteristics } from '@/entities/fact'
import type { RowTone } from '@/entities/fact'
import { useStayDates } from '@/features/stay-dates'
import type { ContractTarget } from '@/features/book-with-wish'
import { RoomsTable } from './RoomsTable'

interface Props {
  hotel: HotelCardData
  wishes?: Desire[]
  // Оценка желаний для этого отеля из последней выдачи поиска; без неё точки нейтральные.
  evals?: WishEval[]
  onBook: (t: ContractTarget) => void
}

const STATUS_TONE: Record<WishStatus, RowTone> = {
  match: 'ok',
  chance: 'warn',
  near: 'warn',
  uncertain: 'warn',
  unknown: 'warn',
  mismatch: 'bad',
}

// Страница отеля: «Главное для вас» (по желаниям сессии), остальные характеристики и номера.
export function HotelFacts({ hotel, wishes, evals, onBook }: Props) {
  const [registry, setRegistry] = useState<Characteristic[]>([])
  const { stay } = useStayDates()
  useEffect(() => {
    getCharacteristics().then(setRegistry, () => setRegistry([]))
  }, [])
  const field = (key: string) => registry.find((c) => c.key === key)?.value_field

  const wishKeys = (wishes ?? []).map((w) => w.key)
  const main = wishKeys.flatMap((k) => hotel.facts.filter((f) => f.key === k))
  const rest = hotel.facts.filter((f) => !wishKeys.includes(f.key))

  // В «Главном» цвет — совпало ли с желанием; спор источников красит точку всегда.
  const toneOf = (f: Fact): RowTone => {
    if (f.conflict) return 'bad'
    const ev = evals?.find((e) => e.key === f.key)
    return ev ? STATUS_TONE[ev.status] : 'neutral'
  }
  const row = (f: Fact, tone: RowTone) => <FactRow key={f.id} fact={f} charKey={f.key} valueField={field(f.key)} tone={tone} stay={stay} />

  return (
    <div className="space-y-5">
      {main.length > 0 && (
        <section className="rounded-card bg-surface p-4">
          <h2 className="text-lg font-semibold">Главное для вас</h2>
          <ul className="mt-1 divide-y divide-line">{main.map((f) => row(f, toneOf(f)))}</ul>
        </section>
      )}

      <details className="rounded-card bg-surface p-4" open={main.length === 0}>
        <summary className="cursor-pointer text-lg font-semibold">Остальные характеристики ({rest.length})</summary>
        <ul className="mt-1 divide-y divide-line">{rest.map((f) => row(f, f.conflict ? 'bad' : 'neutral'))}</ul>
      </details>

      <RoomsTable hotel={hotel} wishes={wishes} onBook={onBook} />

      <p className="text-xs text-ink-muted">
        Методы и идентификаторы свидетельств —{' '}
        <Link to="/admin?role=admin" className="text-brand-link hover:underline">
          под капотом
        </Link>
        .
      </p>
    </div>
  )
}
