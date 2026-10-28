import type { Tone } from '@/shared/ui'
import type { WishEval, WishStatus } from '@/shared/api'
import { pct } from '@/shared/lib'

export const STATUS_TONE: Record<WishStatus, Tone> = {
  match: 'green',
  chance: 'blue',
  near: 'amber',
  uncertain: 'amber',
  mismatch: 'red',
  unknown: 'gray',
}

export const STATUS_TEXT: Record<WishStatus, string> = {
  match: 'text-ok',
  chance: 'text-brand-link',
  near: 'text-warn',
  uncertain: 'text-warn',
  mismatch: 'text-bad',
  unknown: 'text-ink-muted',
}

export function statusLabel(e: Pick<WishEval, 'status' | 'probability' | 'guarantee'>): string {
  switch (e.status) {
    case 'match':
      return e.guarantee ? 'совпало, гарантия' : 'совпало'
    case 'chance':
      return `шанс ${pct(e.probability)}`
    case 'near':
      return 'почти'
    case 'uncertain':
      return 'под вопросом'
    case 'mismatch':
      return 'не совпало'
    case 'unknown':
      return 'неизвестно — спросим отель'
  }
}
