import { useEffect, useState } from 'react'
import { apiGet } from '@/shared/api'
import { Card, ErrorNote, Spinner } from '@/shared/ui'
import { pct } from '@/shared/lib'
import { LlmModeBadge } from '@/features/llm-mode'

interface Summary {
  demo_hotels: number
  coverage: { key: string; hotels: number; avg_confidence: number; low_confidence: number }[]
  open_conflicts: number
  weak_disagreement_facts: number
  stale_facts: number
  questions: Record<string, number>
  hotels_asked: number
  question_share: number
}

const STATUS: Record<string, string> = {
  open: 'ждут ответа',
  answered: 'отвечены',
  needs_clarification: 'нужно уточнение',
  pending: 'ждут очереди',
  pending_low_value: 'копятся',
  closed: 'закрыты',
}

function Tile({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-md bg-slate-50 p-3">
      <div className="text-2xl font-semibold">{value}</div>
      <div className="text-xs text-slate-600">{label}</div>
    </div>
  )
}

// Сводка: счётчики из существующих данных (покрытие, конфликты, вопросы отельерам, режим LLM). Полные метрики — этап 8.
export function SummaryBoard() {
  const [s, setS] = useState<Summary | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    apiGet<Summary>('/admin/summary').then(setS, (e: Error) => setError(e.message))
  }, [])
  if (error) return <ErrorNote>{error}</ErrorNote>
  if (!s) return <Spinner label="Считаю сводку…" />
  const total = Object.values(s.questions).reduce((a, b) => a + b, 0)
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2 text-sm text-slate-700">
        Режим: <LlmModeBadge />
      </div>
      <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
        <Tile label="демо-отелей" value={s.demo_hotels} />
        <Tile label="открытых конфликтов" value={s.open_conflicts} />
        <Tile label="фактов с расхождением источников" value={s.weak_disagreement_facts} />
        <Tile label="фактов на пересчёте" value={s.stale_facts} />
      </div>
      <Card>
        <h3 className="mb-2 font-semibold">Покрытие характеристик уровня отеля</h3>
        <table className="w-full text-sm">
          <thead>
            <tr className="text-left text-xs text-slate-500">
              <th className="py-1">Характеристика</th>
              <th>Отелей с фактом</th>
              <th>Средняя уверенность</th>
              <th>Ниже 75 %</th>
            </tr>
          </thead>
          <tbody>
            {s.coverage.map((c) => (
              <tr key={c.key} className="border-t border-slate-100">
                <td className="py-1">{c.key}</td>
                <td>
                  {c.hotels} из {s.demo_hotels}
                </td>
                <td>{pct(c.avg_confidence)}</td>
                <td>{c.low_confidence}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
      <Card>
        <h3 className="mb-1 font-semibold">Вопросы отельерам</h3>
        <p className="text-sm text-slate-700">
          Всего {total}; хотя бы раз спросили {s.hotels_asked} из {s.demo_hotels} демо-отелей ({pct(s.question_share)}).
        </p>
        <ul className="mt-1 flex flex-wrap gap-3 text-xs text-slate-600">
          {Object.entries(s.questions).map(([k, v]) => (
            <li key={k}>
              {STATUS[k] ?? k}: {v}
            </li>
          ))}
        </ul>
      </Card>
    </section>
  )
}
