import { useEffect, useState } from 'react'
import type { DialogExample } from '@/shared/api'
import { getExamples } from '../api/examplesApi'
import { startExample, useChat } from '../model/store'

// Кнопки-примеры на пустом экране: вход в демо без ключа LLM. Текст уходит в чат без изменений.
export function ExampleButtons() {
  const { busy } = useChat()
  const [examples, setExamples] = useState<DialogExample[]>([])
  useEffect(() => {
    getExamples().then(setExamples, () => setExamples([]))
  }, [])
  if (!examples.length) return null
  return (
    <div className="space-y-2">
      <p className="text-sm text-slate-600">Начните с примера или напишите своими словами:</p>
      <div className="flex flex-col gap-2">
        {examples.map((ex) => (
          <button
            key={ex.id}
            type="button"
            disabled={busy}
            onClick={() => void startExample(ex)}
            className="rounded-lg border border-slate-200 bg-white p-3 text-left text-sm shadow-sm hover:border-sky-400 disabled:opacity-50"
          >
            <span className="block font-medium text-slate-900">{ex.title}</span>
            <span className="text-slate-600">«{ex.text}»</span>
          </button>
        ))}
      </div>
    </div>
  )
}
