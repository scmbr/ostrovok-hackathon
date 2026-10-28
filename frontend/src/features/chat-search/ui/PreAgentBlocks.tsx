import { Link } from 'react-router-dom'
import { Button } from '@/shared/ui'
import { districtLabel } from '@/shared/lib'
import { HotelThumb } from '@/entities/hotel'
import { continueToAgent, useChat } from '../model/store'
import type { CityTurn, LookupTurn, UnknownHotelTurn } from '../model/store'

// «Похоже, вы ищете»: гость мог назвать отель, а не желания. Агенту фраза уходит только по кнопке.
export function LookupBlock({ turn }: { turn: LookupTurn }) {
  const { busy } = useChat()
  return (
    <div className="space-y-2 rounded-inner bg-brand-tint p-3 ring-1 ring-inset ring-brand">
      <p className="text-sm font-medium text-brand-link">Похоже, вы ищете:</p>
      <ul className="space-y-2">
        {turn.hotels.map((h) => (
          <li key={h.id}>
            <Link to={`/hotel/${h.id}`} className="flex items-center gap-3 rounded-card bg-white p-2 ring-1 ring-line hover:ring-brand">
              <HotelThumb hotelId={h.id} name={h.name} />
              <span className="min-w-0 text-sm">
                <span className="block font-semibold text-brand-link">{h.name}</span>
                <span className="text-ink-muted">
                  {districtLabel(h.district)} · {'★'.repeat(h.card.stars)} · {h.card.price_eur} € за ночь
                </span>
              </span>
            </Link>
          </li>
        ))}
      </ul>
      {!turn.resolved && (
        <Button disabled={busy} onClick={() => void continueToAgent(turn.id, turn.message)}>
          Нет, искать по пожеланиям
        </Button>
      )}
    </div>
  )
}

// Город вне охвата: система знает только Барселону, поэтому сперва спрашиваем, искать ли там.
export function CityBlock({ turn }: { turn: CityTurn }) {
  const { busy } = useChat()
  return (
    <div className="space-y-2 rounded-inner bg-warn-bg p-3 text-sm ring-1 ring-inset ring-warn/30">
      <p className="text-warn">Пока работаем только по Барселоне (районы из API). Поискать там?</p>
      {!turn.resolved && (
        <Button variant="primary" disabled={busy} onClick={() => void continueToAgent(turn.id, turn.message)}>
          Искать в Барселоне
        </Button>
      )}
    </div>
  )
}

// Гость назвал отель, которого у нас нет: честно говорим об этом, агент вызывается только по кнопке.
export function UnknownHotelBlock({ turn }: { turn: UnknownHotelTurn }) {
  const { busy } = useChat()
  return (
    <div className="space-y-2 rounded-inner bg-warn-bg p-3 text-sm ring-1 ring-inset ring-warn/30">
      <p className="text-warn">Такого отеля в Барселоне нет в нашей базе.</p>
      {!turn.resolved && (
        <Button disabled={busy} onClick={() => void continueToAgent(turn.id, turn.message)}>
          Искать по пожеланиям
        </Button>
      )}
    </div>
  )
}
