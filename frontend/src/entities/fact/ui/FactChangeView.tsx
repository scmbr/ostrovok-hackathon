import type { FactChange, FactSnap, TrustChange } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { authorLabel, conflictChangeLabel, mainValue, pct, sourceLabel, zeroWeightNote } from '@/shared/lib'

function snapValue(s: FactSnap, field: string, key: string): string {
  return s.found && s.value ? mainValue(s.value, field, key) : 'нет данных'
}

function Side({ title, snap, field, charKey }: { title: string; snap: FactSnap; field: string; charKey: string }) {
  return (
    <div className="flex-1 rounded-inner bg-surface-muted p-2">
      <div className="text-xs uppercase tracking-wide text-ink-muted">{title}</div>
      <div className="text-base font-semibold">{snapValue(snap, field, charKey)}</div>
      <div className="text-xs text-ink">{snap.found ? `уверенность ${pct(snap.confidence)}` : '—'}</div>
      {snap.conflict && <div className="mt-1 text-xs text-bad">{snap.conflict}</div>}
    </div>
  )
}

// Репутация отельера до и после ответа: движок пересчитывает её в том же запросе.
export function TrustChangeView({ trust }: { trust: TrustChange }) {
  const delta = trust.after - trust.before
  const tone = Math.abs(delta) < 0.005 ? 'gray' : delta < 0 ? 'red' : 'green'
  return (
    <div className="flex flex-wrap items-center gap-2 text-sm">
      <span>Доверие к отелю:</span>
      <Badge tone={tone}>
        {trust.before.toFixed(2)} → {trust.after.toFixed(2)}
      </Badge>
      <span className="text-xs text-ink-muted">
        подтверждено расчётом и гостями {trust.agree} из {trust.total}
      </span>
    </div>
  )
}

// Главный кадр кабинета: что записал ответ и что из этого вышло у движка правды, даже если не в пользу отельера.
export function FactChangeView({ change, trust }: { change: FactChange; trust?: TrustChange }) {
  const { before, after } = change
  const weightDropped = change.resolution?.kind === 'weight_dropped'
  const agreed = change.resolution?.kind === 'agreed'
  const same = JSON.stringify(before.value) === JSON.stringify(after.value) && before.confidence === after.confidence
  return (
    <div className="space-y-2 rounded-inner border border-brand bg-brand-tint p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{change.title}</span>
        {change.conflict_change !== 'none' && (
          <Badge tone={change.conflict_change === 'closed' ? (agreed ? 'green' : 'amber') : 'red'}>
            {change.conflict_change === 'closed' && weightDropped ? 'спор снят в пользу расчёта' : conflictChangeLabel(change.conflict_change)}
          </Badge>
        )}
        {same && <Badge tone="gray">значение и уверенность не изменились</Badge>}
      </div>
      {change.conflict_change === 'closed' && change.resolution && (
        <p className={`rounded-inner px-3 py-2 text-sm ${agreed ? 'bg-ok/10 text-ok' : 'bg-warn-bg text-warn'}`}>
          {change.resolution.text}
        </p>
      )}
      <div className="flex items-stretch gap-2">
        <Side title="До" snap={before} field={change.value_field} charKey={change.key} />
        <div className="self-center text-xl text-ink-muted">→</div>
        <Side title="После" snap={after} field={change.value_field} charKey={change.key} />
      </div>
      {after.evidence.length > 0 && (
        <ul className="space-y-0.5 text-xs text-ink">
          {after.evidence.map((e) => (
            <li key={e.id}>
              <b className={e.contribution >= 0 ? 'text-ok' : 'text-bad'}>
                {e.contribution >= 0 ? '+' : ''}
                {e.contribution.toFixed(2)}
              </b>{' '}
              {sourceLabel(e.source)} ({authorLabel(e.author_type)}
              {e.author_type !== 'tool' ? `, репутация ${e.author_trust.toFixed(2)}` : ''}) утверждает «
              {mainValue(e.claimed, change.value_field, change.key)}», база надёжности {pct(e.reliability)}
              {zeroWeightNote(e) && <span className="ml-1 text-warn">— {zeroWeightNote(e)}</span>}
              {e.comment && <span className="ml-1 italic text-ink">— со слов отеля: «{e.comment}»</span>}
            </li>
          ))}
        </ul>
      )}
      {trust && <TrustChangeView trust={trust} />}
    </div>
  )
}
