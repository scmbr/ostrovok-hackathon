import { useEffect, useState } from 'react'
import type { Meta } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { getMeta } from '../api/metaApi'

// Индикатор режима LLM в шапке: ключ задан — живая модель, нет — сохранённые ответы (режим жюри).
export function LlmModeBadge() {
  const [meta, setMeta] = useState<Meta | null>(null)
  useEffect(() => {
    getMeta().then(setMeta, () => setMeta(null))
  }, [])
  if (!meta) return null
  return meta.llm_mode === 'llm' ? (
    <Badge tone="green" title="Агент диалога вызывает живую модель; факты по-прежнему считает код">
      LLM: живой{meta.llm_model ? ` (${meta.llm_model})` : ''}
    </Badge>
  ) : (
    <Badge tone="amber" title="Ключ LLM не задан: примеры отвечают из сохранённых ответов, свободный текст разбирается правилами">
      LLM: кеш
    </Badge>
  )
}

// Плашка под шапкой, только в режиме кеша.
export function LlmModeBanner() {
  const [meta, setMeta] = useState<Meta | null>(null)
  useEffect(() => {
    getMeta().then(setMeta, () => setMeta(null))
  }, [])
  if (meta?.llm_mode !== 'cache') return null
  return (
    <div className="border-b border-amber-200 bg-amber-50 px-4 py-1.5 text-center text-xs text-amber-900">
      Режим без LLM: примеры отвечают из сохранённых ответов, свободный текст разбирается правилами.
    </div>
  )
}
