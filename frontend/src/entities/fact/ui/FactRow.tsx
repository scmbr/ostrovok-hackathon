import { useState } from 'react'
import { Icon } from '@/shared/ui'
import type { IconName } from '@/shared/ui'
import { mainValue, sourceLabel } from '@/shared/lib'
import { FactCard } from './FactCard'
import type { FactLike } from './FactCard'

// Цвет точки. В «Главном для вас» он статусный (совпало с желанием или нет), в «Остальных» нейтральный:
// красная точка у «моря не видно» читалась бы как ошибка, а это просто значение.
export type RowTone = 'ok' | 'warn' | 'bad' | 'neutral'

const DOT: Record<RowTone, string> = { ok: 'bg-ok', warn: 'bg-warn', bad: 'bg-bad', neutral: 'bg-ink-muted' }
const TONE_LABEL: Record<RowTone, string> = { ok: 'совпало', warn: 'шанс или под вопросом', bad: 'не совпало или источники спорят', neutral: '' }

const ICONS: Record<string, IconName> = {
  noise_night: 'noise',
  sea_view: 'sea',
  walk_to_beach: 'sea',
  walk_to_metro: 'metro',
  night_services: 'night',
  night_pharmacy: 'night',
  pets_allowed: 'pet',
}

export const iconFor = (key: string): IconName => ICONS[key] ?? 'hotel'

// Короткая строка факта: иконка, значение человеческими словами, «источник · уверенность».
// Свидетельства, метод и вклады скрыты за «Откуда известно».
export function FactRow({
  fact,
  charKey,
  valueField,
  tone = 'neutral',
  stay,
}: {
  fact: FactLike
  charKey: string
  valueField?: string
  tone?: RowTone
  stay?: { checkin: string; checkout: string }
}) {
  const [open, setOpen] = useState(false)
  return (
    <li className="py-2">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
        <span
          className={`h-2 w-2 shrink-0 rounded-full ${DOT[tone]}`}
          title={TONE_LABEL[tone] || undefined}
          role={tone === 'neutral' ? undefined : 'img'}
          aria-label={TONE_LABEL[tone] || undefined}
        />
        <Icon name={iconFor(charKey)} className="text-ink-muted" />
        <span className="font-medium">{fact.title}</span>
        <span className="text-ink">{mainValue(fact.value, valueField, charKey)}</span>
        <span className="text-xs text-ink-muted">
          {sourceLabel(fact.source)} · {fact.confidence.toFixed(2).replace('.', ',')}
        </span>
        {fact.conflict && <span className="text-xs font-medium text-bad">источники спорят</span>}
        {fact.stale && <span className="text-xs text-warn">устарел</span>}
        {fact.trust_note && (
          <span className="text-xs text-warn" title={fact.trust_note}>
            доверие к отелю снижено
          </span>
        )}
        <button type="button" className="ml-auto text-xs text-brand-link hover:underline" aria-expanded={open} onClick={() => setOpen(!open)}>
          {open ? 'скрыть' : 'откуда известно'}
        </button>
      </div>
      {open && (
        <div className="mt-2">
          <FactCard fact={fact} charKey={charKey} valueField={valueField} stay={stay} />
        </div>
      )}
    </li>
  )
}
