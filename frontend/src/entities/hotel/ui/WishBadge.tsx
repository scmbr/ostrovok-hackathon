import { Badge } from '@/shared/ui'
import type { WishEval } from '@/shared/api'
import { STATUS_TONE, statusLabel } from '../model/wish'

export function WishBadge({ wish }: { wish: WishEval }) {
  return (
    <Badge tone={STATUS_TONE[wish.status]} title={wish.conflict}>
      {statusLabel(wish)}
    </Badge>
  )
}
