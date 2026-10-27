import type { ReactNode } from 'react'
import type { Evidence } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { authorLabel, formatDate, mainValue, pct, sourceLabel } from '@/shared/lib'

export interface FactLike {
  title: string
  value: Record<string, unknown>
  confidence: number
  source: string
  observed_at?: string
  valid_until?: string
  stale?: boolean
  weak_disagreement?: boolean
  conflict?: string
  evidence: Evidence[]
}

export function EvidenceList({ evidence, field, charKey }: { evidence: Evidence[]; field?: string; charKey?: string }) {
  if (!evidence.length) return null
  return (
    <ul className="mt-1 space-y-0.5 text-xs text-slate-700">
      {evidence.map((e) => (
        <li key={e.id}>
          <b className={e.contribution >= 0 ? 'text-emerald-700' : 'text-rose-700'}>
            {e.contribution >= 0 ? '+' : ''}
            {e.contribution.toFixed(2)}
          </b>{' '}
          {sourceLabel(e.source)} ({authorLabel(e.author_type)}) утверждает «{mainValue(e.claimed, field, charKey)}», надёжность {pct(e.reliability)}, {formatDate(e.observed_at)}
        </li>
      ))}
    </ul>
  )
}

// Факт с «откуда известно»: значение, уверенность, источники со вкладом и датой, срок годности, состояние.
export function FactCard({ fact, valueField, charKey, extra }: { fact: FactLike; valueField?: string; charKey?: string; extra?: ReactNode }) {
  const v = mainValue(fact.value, valueField, charKey)
  return (
    <div className="rounded-md border border-slate-200 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{fact.title}</span>
        <span className="text-sm text-slate-800">{v}</span>
        <Badge tone={fact.confidence >= 0.75 ? 'green' : fact.confidence >= 0.5 ? 'amber' : 'red'}>уверенность {pct(fact.confidence)}</Badge>
        {fact.stale && <Badge tone="amber">устарел, ждёт пересчёта</Badge>}
        {fact.conflict && <Badge tone="red">источники спорят</Badge>}
        {fact.weak_disagreement && !fact.conflict && <Badge tone="amber">есть расхождение</Badge>}
        {extra}
      </div>
      <div className="mt-1 text-xs text-slate-500">
        Источник: {sourceLabel(fact.source)}
        {fact.observed_at && <> · получено {formatDate(fact.observed_at)}</>}
        {fact.valid_until && <> · актуально до {formatDate(fact.valid_until)}</>}
      </div>
      {fact.conflict && <p className="mt-1 text-xs text-rose-800">{fact.conflict}</p>}
      <EvidenceList evidence={fact.evidence} field={valueField} charKey={charKey} />
    </div>
  )
}
