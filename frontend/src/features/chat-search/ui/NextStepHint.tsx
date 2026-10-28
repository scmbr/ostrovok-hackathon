import { Button } from '@/shared/ui'
import { nextExampleStep, send, useChat } from '../model/store'

// Подсказка следующего хода демо-диалога из фикстуры. Фраза отправляется как есть (хеш запроса должен совпасть с кешем).
export function NextStepHint() {
  const chat = useChat()
  const step = nextExampleStep(chat)
  if (!step || step.startsWith('@') || chat.busy) return null
  return (
    <div className="flex items-center gap-2 text-sm text-ink">
      <span>Дальше по примеру:</span>
      <Button variant="secondary" onClick={() => void send({ message: step })}>
        «{step}»
      </Button>
    </div>
  )
}
