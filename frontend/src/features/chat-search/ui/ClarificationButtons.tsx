import type { Clarification } from '@/shared/api'
import { Button } from '@/shared/ui'
import { nextExampleStep, send, useChat } from '../model/store'

// Варианты уточнения кнопками. Вариант, совпадающий со следующим ходом демо-диалога, подсвечен: его id берётся из фикстуры.
export function ClarificationButtons({ clarification, answered }: { clarification: Clarification; answered?: string }) {
  const chat = useChat()
  const expected = nextExampleStep(chat)
  return (
    <div className="mt-2 rounded-md bg-sky-50 p-3 ring-1 ring-inset ring-sky-200">
      <p className="text-sm font-medium text-sky-950">{clarification.question}</p>
      <div className="mt-2 flex flex-wrap gap-2">
        {clarification.options.map((o) => (
          <Button
            key={o.id}
            variant={expected === `@${o.id}` && !answered ? 'primary' : 'secondary'}
            disabled={chat.busy || !!answered}
            className={answered === o.id ? 'ring-2 ring-sky-600' : ''}
            onClick={() => void send({ choice: o.id, label: o.label })}
          >
            {o.label}
          </Button>
        ))}
      </div>
    </div>
  )
}
