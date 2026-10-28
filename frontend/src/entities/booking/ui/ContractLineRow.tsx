import type { ReactNode } from 'react'
import type { ContractKind, ContractLine } from '@/shared/api'
import { Badge } from '@/shared/ui'
import type { Tone } from '@/shared/ui'
import { contractLabel, pct } from '@/shared/lib'

const TONES: Record<ContractKind, Tone> = {
  guaranteed: 'green',
  chance: 'blue',
  wish_pending: 'amber',
  wish_confirmed: 'green',
  wish_refused: 'red',
  uncertain: 'amber',
  unknown: 'gray',
  mismatch: 'red',
}

// Пункт контракта ожиданий. Действие («спросить отель») подставляет вызывающий: оно живёт в features.
export function ContractLineRow({ line, action }: { line: ContractLine; action?: ReactNode }) {
  const tone = TONES[line.kind] ?? 'gray'
  return (
    <li className="py-2">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="font-medium">{line.title}</span>
        {line.importance === 'must' && <span className="text-xs text-ink-muted">обязательно</span>}
        <Badge tone={tone}>
          {contractLabel(line.kind)}
          {line.kind === 'chance' ? ` ${pct(line.probability)}` : ''}
        </Badge>
        <span className="text-xs text-ink-muted">уверенность {pct(line.confidence)}</span>
        <span className="ml-auto">{action}</span>
      </div>
      {line.text && <p className="mt-0.5 text-sm text-ink">{line.text}</p>}
      {line.conflict && <p className="mt-0.5 text-xs text-warn">Источники спорят: {line.conflict}</p>}
      {line.source && <p className="mt-0.5 text-xs text-ink-muted">Источник: {line.source}</p>}
    </li>
  )
}
