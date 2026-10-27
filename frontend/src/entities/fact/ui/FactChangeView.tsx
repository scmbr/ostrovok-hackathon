import type { FactChange, FactSnap, TrustChange } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { authorLabel, conflictChangeLabel, mainValue, pct, sourceLabel } from '@/shared/lib'

function snapValue(s: FactSnap, field: string, key: string): string {
  return s.found && s.value ? mainValue(s.value, field, key) : 'нет данных'
}

function Side({ title, snap, field, charKey }: { title: string; snap: FactSnap; field: string; charKey: string }) {
  return (
    <div className="flex-1 rounded-md bg-slate-50 p-2">
      <div className="text-xs uppercase tracking-wide text-slate-500">{title}</div>
      <div className="text-base font-semibold">{snapValue(snap, field, charKey)}</div>
      <div className="text-xs text-slate-600">{snap.found ? `уверенность ${pct(snap.confidence)}` : '—'}</div>
      {snap.conflict && <div className="mt-1 text-xs text-rose-800">{snap.conflict}</div>}
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
      <span className="text-xs text-slate-500">
        подтверждено расчётом и гостями {trust.agree} из {trust.total}
      </span>
    </div>
  )
}

// Главный кадр кабинета: что записал ответ и что из этого вышло у движка правды, даже если не в пользу отельера.
export function FactChangeView({ change, trust }: { change: FactChange; trust?: TrustChange }) {
  const { before, after } = change
  const same = JSON.stringify(before.value) === JSON.stringify(after.value) && before.confidence === after.confidence
  return (
    <div className="space-y-2 rounded-md border border-sky-200 bg-sky-50/50 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{change.title}</span>
        {change.conflict_change !== 'none' && (
          <Badge tone={change.conflict_change === 'closed' ? 'green' : 'red'}>{conflictChangeLabel(change.conflict_change)}</Badge>
        )}
        {same && <Badge tone="gray">значение и уверенность не изменились</Badge>}
      </div>
      <div className="flex items-stretch gap-2">
        <Side title="До" snap={before} field={change.value_field} charKey={change.key} />
        <div className="self-center text-xl text-slate-400">→</div>
        <Side title="После" snap={after} field={change.value_field} charKey={change.key} />
      </div>
      {after.evidence.length > 0 && (
        <ul className="space-y-0.5 text-xs text-slate-700">
          {after.evidence.map((e) => (
            <li key={e.id}>
              <b className={e.contribution >= 0 ? 'text-emerald-700' : 'text-rose-700'}>
                {e.contribution >= 0 ? '+' : ''}
                {e.contribution.toFixed(2)}
              </b>{' '}
              {sourceLabel(e.source)} ({authorLabel(e.author_type)}
              {e.author_type !== 'tool' ? `, репутация ${e.author_trust.toFixed(2)}` : ''}) утверждает «
              {mainValue(e.claimed, change.value_field, change.key)}», надёжность {pct(e.reliability)}
              {e.contribution === 0 && e.reliability <= 0.5 && ' — веса нет: при надёжности не выше 50 % свидетельство ничего не добавляет'}
            </li>
          ))}
        </ul>
      )}
      {trust && <TrustChangeView trust={trust} />}
    </div>
  )
}
