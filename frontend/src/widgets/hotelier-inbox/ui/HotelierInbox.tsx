import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { AnswerOutcome, Inbox, InboxQuestion, MissedDemand, WishRequest } from '@/shared/api'
import { Badge, Card, ErrorNote, Notice, Spinner } from '@/shared/ui'
import { formatDate, mainValue, parsedByLabel, questionStatusLabel } from '@/shared/lib'
import { FactChangeView, TrustChangeView } from '@/entities/fact'
import { getInbox, getMissedDemand } from '@/entities/question'
import { QuestionCard } from '@/features/answer-question'

interface Result {
  key: number
  title: string
  outcome: AnswerOutcome
}

// Результат ответа: разбор, факт до → после, конфликт и доверие. Главный кадр кабинета.
function ResultCard({ r }: { r: Result }) {
  const o = r.outcome
  return (
    <Card className="border-brand">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-semibold">Ответ на «{r.title}»</h3>
        <Badge tone={o.status === 'answered' ? 'green' : 'amber'}>{questionStatusLabel(o.status)}</Badge>
        {o.parsed_by && <span className="text-xs text-ink-muted">{parsedByLabel(o.parsed_by)}</span>}
      </div>
      {o.claimed && (
        <p className="mt-1 text-sm">
          Из ответа разобрано: <b>{mainValue(o.claimed, o.change?.value_field, o.change?.key)}</b>
        </p>
      )}
      {o.note && <p className="text-xs text-ink-muted">{o.note}</p>}
      {o.follow_up && (
        <div className="mt-2">
          <Notice>Значение в ответе не названо, система переспрашивает: {o.follow_up}</Notice>
        </div>
      )}
      {o.change && (
        <div className="mt-2">
          <FactChangeView change={o.change} trust={o.trust} />
        </div>
      )}
      {!o.change && o.trust && (
        <div className="mt-2">
          <TrustChangeView trust={o.trust} />
        </div>
      )}
    </Card>
  )
}

function wishText(w: WishRequest): string {
  const t = w.wishes?.text
  return typeof t === 'string' ? t : 'пожелание к номеру'
}

// Дайджест отельера: вопросы по ценности, счётчик бюджета, пожелания гостей к брони (вне бюджета), упущенный спрос.
export function HotelierInbox({ hotelId, hasPersona }: { hotelId: number; hasPersona: boolean }) {
  const [inbox, setInbox] = useState<Inbox | null>(null)
  const [missed, setMissed] = useState<MissedDemand[]>([])
  const [results, setResults] = useState<Result[]>([])
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    getInbox(hotelId).then(setInbox, (e: Error) => setError(e.message))
    getMissedDemand(hotelId).then(setMissed, () => setMissed([]))
  }, [hotelId])

  useEffect(() => {
    setInbox(null)
    setResults([])
    setError(null)
    load()
  }, [load])

  const onAnswered = (q: InboxQuestion, outcome: AnswerOutcome) => {
    setResults((rs) => [{ key: Date.now(), title: q.title, outcome }, ...rs])
    load()
  }
  const onWishAnswered = (w: WishRequest, outcome: AnswerOutcome) => {
    setResults((rs) => [{ key: Date.now(), title: `Пожелание гостя: ${w.category}`, outcome }, ...rs])
    load()
  }

  if (error) return <ErrorNote>{error}</ErrorNote>
  if (!inbox) return <Spinner label="Загружаю дайджест…" />

  const { budget } = inbox
  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-baseline gap-3">
        <h2 className="text-lg font-semibold">Вопросы от системы</h2>
        <span className="text-sm text-ink">
          На этой неделе {budget.sent} из {budget.limit} вопросов
          {inbox.waiting > 0 && `, ещё ${inbox.waiting} ждут своей очереди`}
          {inbox.low_value > 0 && `, ${inbox.low_value} копятся: пока слишком малая ценность`}
        </span>
      </div>

      {results.map((r) => (
        <ResultCard key={r.key} r={r} />
      ))}

      {inbox.questions.length === 0 && <Notice>Открытых вопросов нет. Новые появятся, когда ценность вопроса превысит порог.</Notice>}
      {inbox.questions.map((q) => (
        <QuestionCard key={`${q.id}-${q.question}`} q={q} hasPersona={hasPersona} onAnswered={onAnswered} />
      ))}

      {inbox.wish_requests.length > 0 && (
        <div className="space-y-2">
          <h2 className="pt-2 text-lg font-semibold">Пожелания гостей к брони</h2>
          <p className="text-xs text-ink-muted">Подтверждения от гостей с заявкой на бронь; в недельный бюджет вопросов не входят.</p>
          {inbox.wish_requests.map((w) => {
            const q: InboxQuestion = {
              id: w.question_id,
              characteristic: 'wish_guarantee',
              title: `Бронь № ${w.booking_id}: ${w.category}`,
              format: 'confirm',
              question: `Гость бронирует «${w.category}» с пожеланием «${wishText(w)}»: можете гарантировать?`,
              reason: `Заявка создана ${formatDate(w.created_at)}. Ответ «нет» снижает вероятность выполнения пожелания у следующих гостей с 70 % до 40 %, «да» повышает до 95 %.`,
              reason_kind: 'no_data',
              value: 1,
              score: { demand: 0, demand_norm: 1, uncertainty: 1, impact: 1, categories_changed: 0, categories: 0 },
              status: 'open',
            }
            return (
              <div key={w.question_id} className="space-y-1">
                <QuestionCard q={q} hasPersona={hasPersona} hideReason onAnswered={(_, o) => onWishAnswered(w, o)} />
                <Link to={`/booking/${w.booking_id}`} className="text-xs text-brand-link hover:underline">
                  Заявка на бронь № {w.booking_id}
                </Link>
              </div>
            )
          })}
        </div>
      )}

      {missed.length > 0 && (
        <div className="space-y-2">
          <h2 className="pt-2 text-lg font-semibold">Упущенный спрос</h2>
          <p className="text-xs text-ink-muted">Гости искали это, а у вашего отеля данных нет или уверенность низкая.</p>
          <ul className="space-y-1 text-sm">
            {missed.map((m) => (
              <li key={m.key} className="rounded-inner bg-warn-bg px-3 py-2 ring-1 ring-inset ring-warn/30">
                {m.text}
                {m.question_id ? <span className="ml-1 text-xs text-ink">— вопрос уже в дайджесте выше</span> : null}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}
