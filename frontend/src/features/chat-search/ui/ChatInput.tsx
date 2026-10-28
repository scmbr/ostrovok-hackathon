import { useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Button } from '@/shared/ui'
import { submit as submitMessage, useChat } from '../model/store'

interface Props {
  // нижняя строка контейнера поиска (даты проживания); фича не знает, что в ней: слайсы друг друга не импортируют
  footer?: ReactNode
  // на главной фраза не уходит в чат, а передаётся наверх (переход на /search); в чате не задаётся
  onSubmit?: (message: string) => void
  submitLabel?: string
}

export function ChatInput({ footer, onSubmit, submitLabel = 'Отправить' }: Props) {
  const { busy } = useChat()
  const [text, setText] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const message = text.trim()
    if (!message || busy) return
    setText('')
    if (onSubmit) onSubmit(message)
    else void submitMessage(message)
  }
  return (
    <form onSubmit={submit} className="rounded-card bg-surface p-1.5 ring-1 ring-line focus-within:ring-brand">
      <div className="flex items-center gap-2">
        <span className="shrink-0 rounded-inner bg-brand-tint px-2 py-1 text-caption font-medium text-brand-link" title="Данные собраны только по Барселоне">
          Барселона
        </span>
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Опишите своими словами, что важно: например, тихо ночью и недалеко от моря"
          className="min-w-0 flex-1 rounded-inner bg-transparent px-3 text-body outline-none placeholder:text-ink-muted"
          maxLength={1000}
          aria-label="Сообщение"
        />
        <Button variant="primary" type="submit" disabled={busy || !text.trim()}>
          {submitLabel}
        </Button>
      </div>
      {footer && <div className="mt-1.5 border-t border-line px-1.5 pt-1.5">{footer}</div>}
    </form>
  )
}
