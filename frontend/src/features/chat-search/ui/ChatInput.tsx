import { useState } from 'react'
import type { FormEvent, ReactNode } from 'react'
import { Button } from '@/shared/ui'
import { send, useChat } from '../model/store'

// footer — нижняя строка контейнера поиска (даты проживания); фича не знает, что в ней: слайсы друг друга не импортируют.
export function ChatInput({ footer }: { footer?: ReactNode }) {
  const { busy } = useChat()
  const [text, setText] = useState('')
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const message = text.trim()
    if (!message || busy) return
    setText('')
    void send({ message })
  }
  return (
    <form onSubmit={submit} className="rounded-card bg-surface p-1.5 ring-1 ring-line focus-within:ring-brand">
      <div className="flex gap-2">
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Опишите своими словами, что важно: например, тихо ночью и недалеко от моря"
          className="min-w-0 flex-1 rounded-inner bg-transparent px-3 text-body outline-none placeholder:text-ink-muted"
          maxLength={1000}
          aria-label="Сообщение"
        />
        <Button variant="primary" type="submit" disabled={busy || !text.trim()}>
          Отправить
        </Button>
      </div>
      {footer && <div className="mt-1.5 border-t border-line px-1.5 pt-1.5">{footer}</div>}
    </form>
  )
}
