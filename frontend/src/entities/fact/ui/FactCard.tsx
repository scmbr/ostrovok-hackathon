import type { ReactNode } from 'react'
import type { Evidence, ExplainedFact } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { ExplainChain } from './ExplainChain'
import { authorLabel, formatDate, mainValue, pct, sourceLabel, zeroWeightNote } from '@/shared/lib'

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
  trust_note?: string
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
          {sourceLabel(e.source)} ({authorLabel(e.author_type)}
          {e.author_trust !== undefined && e.author_type !== 'tool' ? `, репутация ${e.author_trust.toFixed(2)}` : ''}) утверждает «{mainValue(e.claimed, field, charKey)}», база надёжности {pct(e.reliability)}, {formatDate(e.observed_at)}
          {zeroWeightNote(e) && <span className="ml-1 text-amber-800">— {zeroWeightNote(e)}</span>}
          {e.comment && <span className="ml-1 italic text-slate-800">— со слов отеля: «{e.comment}»</span>}
        </li>
      ))}
    </ul>
  )
}

// Факт с «откуда известно»: значение, уверенность, источники со вкладом и датой, срок годности, состояние.
export function FactCard({
  fact,
  valueField,
  charKey,
  extra,
  explain,
}: {
  fact: FactLike
  valueField?: string
  charKey?: string
  extra?: ReactNode
  // «Под капотом»: разбор факта по свидетельствам (цепочка надёжности и вклады) вместо короткого списка источников.
  explain?: ExplainedFact
}) {
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
      {fact.trust_note && <p className="mt-1 text-xs text-amber-800">{fact.trust_note}</p>}
      {explain ? <ExplainChain explain={explain} /> : <EvidenceList evidence={fact.evidence} field={valueField} charKey={charKey} />}
    </div>
  )
}
