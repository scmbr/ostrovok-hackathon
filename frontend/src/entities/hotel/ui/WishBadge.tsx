import { Icon } from '@/shared/ui'
import type { IconName } from '@/shared/ui'
import type { WishEval, WishStatus } from '@/shared/api'
import { STATUS_TEXT, statusLabel } from '../model/wish'

const STATUS_ICON: Record<WishStatus, IconName> = {
  match: 'check',
  chance: 'question',
  near: 'question',
  uncertain: 'question',
  mismatch: 'cross',
  unknown: 'question',
}

// Статус желания: иконка и текст цвета статуса (ok / warn / bad / ink), как пункты списка в макете.
export function WishBadge({ wish }: { wish: WishEval }) {
  return (
    <span className={`inline-flex items-center gap-1.5 text-caption font-medium ${STATUS_TEXT[wish.status]}`} title={wish.conflict}>
      <Icon name={wish.conflict ? 'conflict' : STATUS_ICON[wish.status]} />
      {statusLabel(wish)}
    </span>
  )
}
