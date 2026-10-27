import type { PlacedWish } from '@/shared/api'
import { Badge } from '@/shared/ui'
import type { Tone } from '@/shared/ui'
import { bookingWishLabel } from '@/shared/lib'

const TONES: Record<PlacedWish['status'], Tone> = { waiting: 'amber', confirmed: 'green', refused: 'red' }

export function WishStatusBadge({ status }: { status: PlacedWish['status'] }) {
  return <Badge tone={TONES[status] ?? 'gray'}>{bookingWishLabel(status)}</Badge>
}
