import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import type { AdminConflict } from '@/shared/api'
import { Badge, Card, ErrorNote, Spinner } from '@/shared/ui'
import { authorLabel, formatDate, resolutionKindLabel, sourceLabel, valueLabel } from '@/shared/lib'
import { getConflicts } from '@/entities/conflict'
import type { ConflictBoard } from '@/entities/conflict'

const ENTITY_LABEL: Record<string, string> = { hotel: 'отель', facade: 'фасад', room_category: 'категория номеров' }
const n2 = (x: number) => x.toFixed(2).replace('.', ',')

function ConflictCard({ c }: { c: AdminConflict }) {
  const open = c.status === 'open'
  const max = Math.max(...c.sides.map((s) => s.weight), 0.01)
  const cabinet = c.hotel_id ? `/hotelier?role=hotelier&hotel=${c.hotel_id}` : null
  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-semibold">{c.title}</h3>
        <Badge tone="gray">{`${ENTITY_LABEL[c.entity_type] ?? c.entity_type} ${c.entity_id}`}</Badge>
        {c.hotel_id ? (
          <Link to={`/hotel/${c.hotel_id}`} className="text-xs text-sky-800 hover:underline">
            отель {c.hotel_id}
          </Link>
        ) : null}
        <Badge tone={open ? 'red' : c.resolution_kind === 'agreed' ? 'green' : 'amber'}>
          {open ? 'открыт' : resolutionKindLabel(c.resolution_kind ?? '')}
        </Badge>
        <span className="ml-auto text-xs text-slate-500">{formatDate(c.detected_at)}</span>
      </div>
      {open ? (
        <div className="mt-2 space-y-2">
          {c.sides.map((s) => (
            <div key={s.value}>
              <div className="flex items-center gap-2 text-sm">
                <b>{valueLabel(c.key, s.value === 'true' ? true : s.value === 'false' ? false : s.value)}</b>
                <span className="text-xs text-slate-600">вес {n2(s.weight)}</span>
                <div className="h-2 flex-1 rounded bg-slate-100">
                  <div className="h-2 rounded bg-sky-600" style={{ width: `${Math.min(100, (s.weight / max) * 100)}%` }} />
                </div>
              </div>
              <ul className="ml-3 text-xs text-slate-700">
                {s.evidence.map((e, i) => (
                  <li key={i}>
                    {sourceLabel(e.source)} ({authorLabel(e.author_type)}
                    {e.author_type !== 'tool' ? ` ${e.author_id}` : ''}): вклад {n2(Math.abs(e.contribution))}
                    {e.zero_weight && <span className="text-amber-800"> — вес ≈ 0</span>}
                    {e.comment && <span className="italic"> — со слов отеля: «{e.comment}»</span>}
                  </li>
                ))}
              </ul>
            </div>
          ))}
          {c.theta_reason && (
            <p className={`text-xs ${c.theta_lowered ? 'text-amber-900' : 'text-slate-600'}`}>
              Порог конфликта: {c.theta_reason}. Конфликт, если обе стороны весят не меньше θ.
            </p>
          )}
        </div>
      ) : (
        <p className="mt-2 text-sm text-slate-700">{c.resolution ? c.resolution.charAt(0).toUpperCase() + c.resolution.slice(1) : c.description}</p>
      )}
      <div className="mt-2 flex flex-wrap gap-3 text-xs">
        {c.question && cabinet && (
          <Link to={cabinet} className="text-sky-800 hover:underline">
            вопрос отельеру № {c.question.id} ({c.question.status})
          </Link>
        )}
        {!c.question && open && cabinet && <span className="text-slate-500">вопроса отельеру нет</span>}
      </div>
    </Card>
  )
}

// Конфликты: стороны с весами, применённый порог θ и почему, связанный вопрос отельеру, задачи пересчёта и устаревшие факты.
export function ConflictsBoard() {
  const [status, setStatus] = useState<'open' | 'closed' | 'all'>('open')
  const [board, setBoard] = useState<ConflictBoard | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    setBoard(null)
    setError(null)
    getConflicts(status).then(setBoard, (e: Error) => setError(e.message))
  }, [status])

  return (
    <section className="space-y-3">
      <div className="flex gap-1 text-sm">
        {(['open', 'closed', 'all'] as const).map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => setStatus(s)}
            className={`rounded-md px-2 py-1 ${s === status ? 'bg-sky-700 text-white' : 'text-slate-700 hover:bg-slate-100'}`}
          >
            {{ open: 'открытые', closed: 'закрытые', all: 'все' }[s]}
          </button>
        ))}
      </div>
      {error && <ErrorNote>{error}</ErrorNote>}
      {!board && !error && <Spinner label="Загружаю конфликты…" />}
      {board && board.conflicts.length === 0 && <p className="text-sm text-slate-500">Конфликтов нет.</p>}
      {board?.conflicts.map((c) => <ConflictCard key={c.id} c={c} />)}

      {board && board.tasks.length > 0 && (
        <div>
          <h3 className="mb-1 font-semibold">Задачи пересчёта (сигнал гостей)</h3>
          <ul className="space-y-1 text-sm">
            {board.tasks.map((t) => (
              <li key={t.id} className="rounded-md border border-slate-200 px-3 py-2">
                отель {t.hotel_id}, {t.key}: {t.reason}{' '}
                <Badge tone={t.status === 'open' ? 'amber' : 'green'}>{t.status === 'open' ? 'открыта' : `выполнена: ${t.outcome ?? ''}`}</Badge>
              </li>
            ))}
          </ul>
        </div>
      )}
      {board && (
        <div>
          <h3 className="mb-1 font-semibold">Устаревшие факты</h3>
          {board.stale.length === 0 ? (
            <p className="text-sm text-slate-500">Устаревших фактов нет.</p>
          ) : (
            <ul className="text-sm">
              {board.stale.map((f, i) => (
                <li key={i}>
                  {f.key} · {ENTITY_LABEL[f.entity_type] ?? f.entity_type} {f.entity_id}
                  {f.valid_until ? `, срок до ${formatDate(f.valid_until)}` : ''}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  )
}
