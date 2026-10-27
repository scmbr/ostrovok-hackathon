import type { PlacedWish } from '@/shared/api'
import { Badge } from '@/shared/ui'

const STATUS = {
  waiting: { label: 'ждём ответа отеля', tone: 'amber' },
  confirmed: { label: 'отель подтвердил', tone: 'green' },
  refused: { label: 'отель не гарантирует', tone: 'red' },
} as const

export function WishStatusBadge({ status }: { status: PlacedWish['status'] }) {
  const s = STATUS[status]
  return <Badge tone={s.tone}>{s.label}</Badge>
}
