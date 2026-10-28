import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import type { Compare, ResultsEvent, StepEvent } from '@/shared/api'
import { Badge, Button, ErrorNote, Spinner } from '@/shared/ui'
import { useSessionNotice } from '@/entities/session'
import { ChatInput, ClarificationButtons, ExampleButtons, NextStepHint, resetChat, unmetFromMessage, useChat } from '@/features/chat-search'
import type { AgentTurn } from '@/features/chat-search'
import { StayDatesPicker } from '@/features/stay-dates'

interface Props {
  // Выдачу и сравнение рисуют виджеты выше по странице: виджеты друг друга не импортируют.
  renderResults: (r: ResultsEvent, unmet: string[]) => ReactNode
  renderCompare: (c: Compare) => ReactNode
}

function stepLine(s: StepEvent): string {
  return s.tool ? `${s.decision} (${s.tool})` : s.decision
}

// Лента шагов агента: пока идёт ход — живая, после done сворачивается в «что делал агент».
function Steps({ turn }: { turn: AgentTurn }) {
  if (!turn.steps.length) return turn.running ? <Spinner label="Разбираю запрос…" /> : null
  if (turn.running) {
    return (
      <ol className="space-y-0.5 text-xs text-ink" aria-live="polite">
        {turn.steps.map((s, i) => (
          <li key={i}>→ {stepLine(s)}</li>
        ))}
        <li>
          <Spinner />
        </li>
      </ol>
    )
  }
  return (
    <details className="text-xs text-ink-muted">
      <summary className="cursor-pointer">Что делал агент: {turn.steps.length} шагов{turn.done ? `, режим ${turn.done.mode}` : ''}</summary>
      <ol className="mt-1 space-y-0.5">
        {turn.steps.map((s, i) => (
          <li key={i}>
            {i + 1}. {stepLine(s)}
          </li>
        ))}
      </ol>
    </details>
  )
}

const LONG_MESSAGE = 420

// Длинный текст ответа повторяет карточки ниже: показываем начало, остальное — по раскрытию.
function MessageText({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  if (text.length <= LONG_MESSAGE || open) {
    return (
      <>
        <p className="whitespace-pre-line">{text}</p>
        {open && (
          <button type="button" className="mt-1 text-xs text-brand-link hover:underline" onClick={() => setOpen(false)}>
            свернуть
          </button>
        )}
      </>
    )
  }
  const cut = text.lastIndexOf(' ', LONG_MESSAGE)
  return (
    <>
      <p className="whitespace-pre-line">{text.slice(0, cut > 0 ? cut : LONG_MESSAGE)}…</p>
      <button type="button" className="mt-1 text-xs text-brand-link hover:underline" onClick={() => setOpen(true)}>
        показать полностью
      </button>
    </>
  )
}

function AgentBubble({ turn, renderResults, renderCompare }: { turn: AgentTurn } & Props) {
  return (
    <div className="space-y-2">
      <Steps turn={turn} />
      {turn.message && (
        <div className="rounded-card bg-white p-3 text-sm  ring-1 ring-line">
          <MessageText text={turn.message.text} />
          <p className="mt-1">
            <Badge tone={turn.message.source === 'llm' ? 'violet' : 'gray'} title="Числа и названия в тексте проверены кодом; факты считает код, не модель">
              {turn.message.source === 'llm' ? 'пересказ модели, проверен кодом' : 'шаблон'}
            </Badge>
          </p>
        </div>
      )}
      {turn.clarification && <ClarificationButtons clarification={turn.clarification} answered={turn.answered} />}
      {turn.results && renderResults(turn.results, unmetFromMessage(turn.message?.text))}
      {turn.compare && renderCompare(turn.compare)}
      {turn.error && <ErrorNote>{turn.error}</ErrorNote>}
    </div>
  )
}

export function ChatPanel(props: Props) {
  const chat = useChat()
  const [notice, clearNotice] = useSessionNotice()
  const lastUser = useRef<HTMLDivElement>(null)
  // Новая реплика гостя — к её началу: выдача читается сверху вниз, а не с конца.
  const lastUserId = [...chat.turns].reverse().find((t) => t.kind === 'user')?.id
  const userTurns = chat.turns.filter((t) => t.kind === 'user').length
  useEffect(() => {
    if (userTurns > 0) lastUser.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' })
  }, [userTurns])

  return (
    <div className="flex flex-col gap-4">
      {notice && (
        <div className="flex items-center gap-2 rounded-inner bg-warn-bg px-3 py-2 text-sm text-warn ring-1 ring-inset ring-warn/30">
          <span>{notice}</span>
          <Button small variant="ghost" className="ml-auto" onClick={clearNotice}>
            Понятно
          </Button>
        </div>
      )}
      {chat.turns.length === 0 ? (
        <ExampleButtons />
      ) : (
        <div className="space-y-4">
          {chat.turns.map((t) =>
            t.kind === 'user' ? (
              <div
                key={t.id}
                ref={t.id === lastUserId ? lastUser : undefined}
                className="ml-auto max-w-[85%] scroll-mt-28 rounded-card bg-brand px-3 py-2 text-sm text-white"
              >
                {t.text}
              </div>
            ) : (
              <AgentBubble key={t.id} turn={t} {...props} />
            ),
          )}
        </div>
      )}
      <div className="sticky bottom-0 space-y-2 bg-surface-muted pb-2 pt-1">
        <NextStepHint />
        <ChatInput footer={<StayDatesPicker />} />
        {chat.turns.length > 0 && (
          <Button small variant="ghost" onClick={() => resetChat()} title="Начнёт новую сессию: желания и шортлист сбросятся">
            Новый поиск
          </Button>
        )}
      </div>
    </div>
  )
}
