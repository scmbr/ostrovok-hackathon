import { useEffect, useState } from 'react'
import type { DialogExample } from '@/shared/api'
import { getExamples } from '../api/examplesApi'
import { useChat } from '../model/store'

// Чипы-примеры на главной: вход в демо без ключа LLM. Выбранный пример отдаётся наверх целиком,
// текст уходит в чат байт в байт (иначе хеш запроса не совпадёт с кешем диалога).
export function ExampleChips({ onPick }: { onPick: (ex: DialogExample) => void }) {
  const { busy } = useChat()
  const [examples, setExamples] = useState<DialogExample[]>([])
  useEffect(() => {
    getExamples().then(setExamples, () => setExamples([]))
  }, [])
  if (!examples.length) return null
  return (
    <ul className="flex flex-wrap justify-center gap-2" aria-label="Примеры запросов">
      {examples.map((ex) => (
        <li key={ex.id}>
          <button
            type="button"
            disabled={busy}
            title={ex.text}
            onClick={() => onPick(ex)}
            className="rounded-full bg-brand-soft px-3 py-1.5 text-body font-medium text-brand-link hover:bg-brand-tint disabled:opacity-50"
          >
            {ex.title}
          </button>
        </li>
      ))}
    </ul>
  )
}
