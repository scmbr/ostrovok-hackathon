import type { ReactNode } from 'react'
import type { ContractKind, ContractLine } from '@/shared/api'
import { Badge } from '@/shared/ui'
import type { Tone } from '@/shared/ui'
import { pct } from '@/shared/lib'

const KINDS: Record<ContractKind, { label: string; tone: Tone }> = {
  guaranteed: { label: 'гарантировано', tone: 'green' },
  chance: { label: 'шанс', tone: 'blue' },
  wish_pending: { label: 'пожелание: ждём отель', tone: 'amber' },
  wish_confirmed: { label: 'пожелание: отель подтвердил', tone: 'green' },
  wish_refused: { label: 'пожелание: отель не гарантирует', tone: 'red' },
  uncertain: { label: 'под вопросом', tone: 'amber' },
  unknown: { label: 'неизвестно', tone: 'gray' },
  mismatch: { label: 'не совпадает', tone: 'red' },
}

// Пункт контракта ожиданий. Действие («спросить отель») подставляет вызывающий: оно живёт в features.
export function ContractLineRow({ line, action }: { line: ContractLine; action?: ReactNode }) {
  const k = KINDS[line.kind] ?? { label: line.kind, tone: 'gray' as Tone }
  return (
    <li className="py-2">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium">{line.title}</span>
        {line.importance === 'must' && <span className="text-xs text-slate-500">обязательно</span>}
        <Badge tone={k.tone}>
          {k.label}
          {line.kind === 'chance' ? ` ${pct(line.probability)}` : ''}
        </Badge>
        <span className="text-xs text-slate-500">уверенность {pct(line.confidence)}</span>
        <span className="ml-auto">{action}</span>
      </div>
      {line.text && <p className="mt-0.5 text-sm text-slate-700">{line.text}</p>}
      {line.conflict && <p className="mt-0.5 text-xs text-amber-800">Источники спорят: {line.conflict}</p>}
      {line.source && <p className="mt-0.5 text-xs text-slate-500">Источник: {line.source}</p>}
    </li>
  )
}
