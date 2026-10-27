import type { ReasonKind } from '@/shared/api'
import { Badge } from '@/shared/ui'
import type { Tone } from '@/shared/ui'
import { reasonKindLabel } from '@/shared/lib'

const TONE: Record<ReasonKind, Tone> = { no_data: 'gray', low_confidence: 'amber', conflict: 'red', guests: 'violet' }

export function ReasonBadge({ kind }: { kind: ReasonKind }) {
  return <Badge tone={TONE[kind] ?? 'gray'}>{reasonKindLabel(kind)}</Badge>
}
