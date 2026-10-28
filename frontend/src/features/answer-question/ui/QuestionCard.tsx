import { useState } from 'react'
import type { AnswerOutcome, InboxQuestion, PersonaPreview } from '@/shared/api'
import { Badge, Button, Card, ErrorNote } from '@/shared/ui'
import { mainValue, parsedByLabel, personaLabel } from '@/shared/lib'
import { ReasonBadge } from '@/entities/question'
import { answerQuestion, previewPersona } from '../api/answerApi'
import type { AnswerBody } from '../api/answerApi'

interface Props {
  q: InboxQuestion
  // Есть ли у отеля имитированная персона: только тогда доступна кнопка «ответить как персона».
  hasPersona: boolean
  // Не показывать причину-бейдж (подтверждение пожелания из брони: причина — сама бронь).
  hideReason?: boolean
  onAnswered: (q: InboxQuestion, outcome: AnswerOutcome) => void
}

// Разбор ответа персоны честно говорит, если его нельзя получить без модели.
function PreviewBox({ preview, onSend, busy }: { preview: PersonaPreview; onSend: () => void; busy: boolean }) {
  const resolved = preview.found || !!preview.follow_up
  return (
    <div className="mt-2 rounded-inner border border-brand bg-brand-soft p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone="violet">имитация: {personaLabel(preview.persona)}</Badge>
        <span className="text-xs text-ink">{parsedByLabel(preview.parsed_by)}</span>
      </div>
      <p className="mt-1 italic">«{preview.answer}»</p>
      {preview.found && preview.claimed && (
        <p className="mt-1 text-xs text-ink">
          Из ответа разобрано: <b>{mainValue(preview.claimed)}</b>
          {preview.note ? ` (${preview.note})` : ''}
        </p>
      )}
      {preview.follow_up && <p className="mt-1 text-xs text-ink">Ответ не задаёт значения: система переспросит.</p>}
      {!resolved && (
        <p className="mt-1 text-xs text-warn">
          Ответ персоны не удалось разобрать{preview.note ? ` (${preview.note})` : ''}. Если отправить его как есть, вопрос получит статус «нужно
          уточнение»; можно ответить кнопками или текстом вручную.
        </p>
      )}
      <Button small variant="primary" className="mt-2" disabled={busy} onClick={onSend}>
        Отправить этот ответ
      </Button>
    </div>
  )
}

export function QuestionCard({ q, hasPersona, hideReason, onAnswered }: Props) {
  const [text, setText] = useState('')
  const [comment, setComment] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [preview, setPreview] = useState<PersonaPreview | null>(null)

  const send = async (body: AnswerBody) => {
    setBusy(true)
    setError(null)
    try {
      onAnswered(q, await answerQuestion(q.id, body))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const askPersona = async () => {
    setBusy(true)
    setError(null)
    try {
      setPreview(await previewPersona(q.id))
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card>
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="font-semibold">{q.title}</h3>
        {!hideReason && <ReasonBadge kind={q.reason_kind} />}
        {q.score.demand > 0 && <Badge tone="blue">{q.score.demand} гостей искали</Badge>}
        {!hideReason && (
          <Badge tone="gray" title="спрос × неуверенность × влияние">
            ценность {q.value.toFixed(2)}
          </Badge>
        )}
        {q.status === 'needs_clarification' && <Badge tone="amber">нужно уточнение</Badge>}
      </div>
      <p className="mt-2 text-sm">{q.question}</p>
      <p className="mt-1 text-xs text-ink-muted">{q.reason}</p>

      <div className="mt-3 space-y-2">
        {q.format === 'confirm' && (
          <input
            value={comment}
            maxLength={500}
            onChange={(e) => setComment(e.target.value)}
            placeholder="Пояснение к «да» / «нет» (необязательно): оно сохранится рядом с ответом и не влияет на значение"
            className="w-full rounded-inner border border-line px-2 py-1 text-sm"
          />
        )}
        {q.format === 'confirm' && (
          <div className="flex flex-wrap gap-2">
            <Button small variant="primary" disabled={busy} onClick={() => send({ answer: 'yes', comment: comment.trim() || undefined })}>
              Да
            </Button>
            <Button small disabled={busy} onClick={() => send({ answer: 'no', comment: comment.trim() || undefined })}>
              Нет
            </Button>
            <Button small variant="ghost" disabled={busy} onClick={() => send({ text: 'уточнить' })}>
              Уточнить
            </Button>
          </div>
        )}
        <div className="flex gap-2">
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={q.format === 'confirm' ? 'Или ответьте текстом…' : 'Ответ своими словами…'}
            className="min-w-0 flex-1 rounded-inner border border-line px-2 py-1 text-sm"
            onKeyDown={(e) => e.key === 'Enter' && text.trim() && send({ text })}
          />
          <Button small variant={q.format === 'confirm' ? 'secondary' : 'primary'} disabled={busy || !text.trim()} onClick={() => send({ text })}>
            Отправить
          </Button>
        </div>
        {hasPersona && (
          <Button small variant="ghost" disabled={busy} onClick={askPersona}>
            Ответить как персона (имитация)
          </Button>
        )}
        {preview && <PreviewBox preview={preview} busy={busy} onSend={() => send({ text: preview.answer })} />}
        {error && <ErrorNote>{error}</ErrorNote>}
      </div>
    </Card>
  )
}
