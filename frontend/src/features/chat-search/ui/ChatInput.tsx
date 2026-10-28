import { useState } from 'react'
import type { FormEvent } from 'react'
import { Button } from '@/shared/ui'
import { send, useChat } from '../model/store'

export function ChatInput() {
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
    <form onSubmit={submit} className="flex gap-2">
      <input
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="Опишите своими словами, что важно: например, тихо ночью и недалеко от моря"
        className="min-w-0 flex-1 rounded-inner border border-line px-3 py-2 text-sm focus:border-brand focus:outline-none"
        maxLength={1000}
        aria-label="Сообщение"
      />
      <Button variant="primary" type="submit" disabled={busy || !text.trim()}>
        Отправить
      </Button>
    </form>
  )
}
