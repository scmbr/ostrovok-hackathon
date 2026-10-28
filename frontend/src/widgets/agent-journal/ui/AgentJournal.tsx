import { useEffect, useState } from 'react'
import type { AgentRun, AgentRunStep } from '@/shared/api'
import { Badge, Card, ErrorNote, Spinner } from '@/shared/ui'
import { getRuns, getRunSteps } from '@/entities/agent-run'

const AGENTS: Record<string, string> = {
  '': 'все агенты',
  collector: 'сборщик',
  hotelier_interviewer: 'интервьюер отельера',
  guest_dialog: 'диалог с гостем',
  guest_signal: 'сигнал гостей',
}

const MODE_TONE: Record<string, 'green' | 'blue' | 'gray' | 'violet'> = { llm: 'green', cache: 'blue', planner: 'gray', rule: 'violet' }
const MODE_LABEL: Record<string, string> = { llm: 'LLM', cache: 'кеш', planner: 'планировщик', rule: 'правило' }

const time = (iso: string) => new Date(iso).toLocaleString('ru-RU', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

function Json({ label, value }: { label: string; value: Record<string, unknown> }) {
  if (!value || Object.keys(value).length === 0) return null
  return (
    <details className="text-xs">
      <summary className="cursor-pointer text-ink-muted">{label}</summary>
      <pre className="mt-1 max-h-60 overflow-auto rounded bg-surface-muted p-2 text-[11px]">{JSON.stringify(value, null, 1)}</pre>
    </details>
  )
}

function Steps({ run }: { run: AgentRun }) {
  const [steps, setSteps] = useState<AgentRunStep[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    getRunSteps(run.session_id).then(setSteps, (e: Error) => setError(e.message))
  }, [run.session_id])
  if (error) return <ErrorNote>{error}</ErrorNote>
  if (!steps) return <Spinner label="Загружаю шаги…" />
  return (
    <ol className="mt-2 space-y-1.5 border-l-2 border-line pl-3">
      {steps.map((s, i) => (
        <li key={`${s.step}-${i}`} className={s.refusal ? 'rounded-inner bg-bad-bg px-2 py-1 ring-1 ring-inset ring-bad/30' : ''}>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-xs text-ink-muted">{s.step}</span>
            {s.refusal && <Badge tone="red">отказ</Badge>}
            <span className={s.refusal ? 'text-bad' : ''}>{s.decision}</span>
            {s.tool && <Badge tone="gray">{s.tool}</Badge>}
            <Badge tone={MODE_TONE[s.mode] ?? 'gray'}>{MODE_LABEL[s.mode] ?? s.mode}</Badge>
          </div>
          <Json label="input" value={s.input} />
          <Json label="output" value={s.output} />
        </li>
      ))}
    </ol>
  )
}

// Журнал агентов: прогоны (агент, отель или сессия, режим, шаги, отказы) и шаги выбранного прогона лентой.
export function AgentJournal() {
  const [agent, setAgent] = useState('')
  const [hotel, setHotel] = useState('')
  const [runs, setRuns] = useState<AgentRun[] | null>(null)
  const [open, setOpen] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setRuns(null)
    setError(null)
    const hotelId = Number(hotel)
    getRuns({ agent: agent || undefined, hotelId: hotelId > 0 ? hotelId : undefined }).then(setRuns, (e: Error) => setError(e.message))
  }, [agent, hotel])

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <label className="flex items-center gap-1">
          Агент
          <select value={agent} onChange={(e) => setAgent(e.target.value)} className="rounded-inner border border-line bg-white px-2 py-1">
            {Object.entries(AGENTS).map(([k, v]) => (
              <option key={k} value={k}>
                {v}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center gap-1">
          Отель
          <input
            value={hotel}
            onChange={(e) => setHotel(e.target.value.replace(/\D/g, ''))}
            placeholder="id"
            className="w-20 rounded-inner border border-line px-2 py-1"
          />
        </label>
        {runs && <span className="text-xs text-ink-muted">прогонов: {runs.length}</span>}
      </div>
      {error && <ErrorNote>{error}</ErrorNote>}
      {!runs && !error && <Spinner label="Загружаю журнал…" />}
      {runs && runs.length === 0 && <p className="text-sm text-ink-muted">Прогонов нет.</p>}
      {runs?.map((r) => (
        <Card key={r.session_id} className="!p-3">
          <button type="button" className="flex w-full flex-wrap items-center gap-2 text-left" onClick={() => setOpen(open === r.session_id ? null : r.session_id)}>
            <span className="text-xs text-ink-muted">{time(r.started_at)}</span>
            <b className="text-sm">{AGENTS[r.agent] ?? r.agent}</b>
            <span className="text-xs text-ink">{r.hotel_id ? `отель ${r.hotel_id}` : `сессия ${r.session_id.slice(0, 12)}`}</span>
            <Badge tone={MODE_TONE[r.mode] ?? 'gray'}>{MODE_LABEL[r.mode] ?? r.mode}</Badge>
            <Badge tone="gray">{r.steps} шагов</Badge>
            {r.refusals > 0 && <Badge tone="red">{r.refusals} отказов</Badge>}
            <span className="min-w-0 flex-1 truncate text-xs text-ink-muted">{r.title}</span>
          </button>
          {open === r.session_id && <Steps run={r} />}
        </Card>
      ))}
    </section>
  )
}
