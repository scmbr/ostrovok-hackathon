import { useCallback, useEffect, useState } from 'react'
import type { Characteristic } from '@/shared/api'
import { Badge, Card, ErrorNote, Spinner } from '@/shared/ui'
import { formatDate, hasLabels } from '@/shared/lib'
import { getCharacteristics, resetCharacteristics } from '@/entities/fact'
import { AddCharacteristic } from '@/features/add-characteristic'

const LEVEL: Record<string, string> = { hotel: 'отель', facade: 'фасад', district: 'район' }
const TYPE: Record<string, string> = { bool: 'да / нет', enum: 'шкала', number: 'число', range: 'диапазон', text: 'структура' }

function Row({ c }: { c: Characteristic }) {
  return (
    <Card className="!p-3">
      <div className="flex flex-wrap items-center gap-2">
        <b>{c.title}</b>
        <Badge tone="gray">{c.key}</Badge>
        <Badge tone="blue">{LEVEL[c.level] ?? c.level}</Badge>
        <Badge tone="gray">{TYPE[c.value_type] ?? c.value_type}</Badge>
        {c.ttl_days ? <span className="text-xs text-slate-500">срок годности {c.ttl_days} дн.</span> : null}
        <Badge tone={c.origin === 'text' ? 'violet' : 'gray'}>{c.origin === 'text' ? 'добавлена текстом' : 'YAML'}</Badge>
        {c.origin === 'text' && !hasLabels(c.key) && <Badge tone="amber">подпись не задана</Badge>}
      </div>
      <p className="mt-1 text-xs text-slate-600">{c.description}</p>
      {c.origin === 'text' && (
        <p className="mt-1 text-xs text-slate-700">
          Исходный текст: «{c.origin_text}»{c.added_at ? `, ${formatDate(c.added_at)}` : ''}
          {c.synonyms_origin ? ` · синонимы: ${c.synonyms_origin === 'derived' ? 'предложены кодом' : 'отредактированы'}` : ''}
        </p>
      )}
      {c.strategies && c.strategies.length > 0 ? (
        <ul className="mt-1 text-xs text-slate-700">
          {c.strategies.map((s, i) => (
            <li key={i}>
              {s.tool}
              {s.strategy ? `:${s.strategy}` : ''} · база {s.base_reliability.toFixed(2).replace('.', ',')} · группа {s.independence_group}
            </li>
          ))}
        </ul>
      ) : (
        <p className="mt-1 text-xs text-slate-500">Расчёта нет: значение только от отельера.</p>
      )}
      {c.ask_hotelier && <p className="mt-1 text-xs text-slate-600">Вопрос отельеру: {c.ask_hotelier}</p>}
      {c.synonyms && c.synonyms.length > 0 && <p className="mt-1 text-xs text-slate-500">Синонимы: {c.synonyms.join(', ')}</p>}
    </Card>
  )
}

// Реестр характеристик: уровень, тип, стратегии с базами и группами, срок годности, вопрос отельеру, происхождение; добавление текстом.
export function RegistryBoard({ initialText }: { initialText?: string }) {
  const [list, setList] = useState<Characteristic[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(() => {
    resetCharacteristics()
    getCharacteristics().then(setList, (e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])

  return (
    <section className="space-y-3">
      <AddCharacteristic onSaved={load} initialText={initialText} />
      {error && <ErrorNote>{error}</ErrorNote>}
      {!list && !error && <Spinner label="Загружаю реестр…" />}
      <div className="space-y-2">{list?.map((c) => <Row key={c.key} c={c} />)}</div>
    </section>
  )
}
