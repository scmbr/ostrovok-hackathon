import { apiPost } from '@/shared/api'
import type { AnswerOutcome, PersonaPreview } from '@/shared/api'

export type AnswerBody = { text: string } | { answer: 'yes' | 'no' }

export const answerQuestion = (id: number, body: AnswerBody) => apiPost<AnswerOutcome>(`/hotelier/questions/${id}/answer`, body)

// Ответ имитированной персоны и его разбор; в БД ничего не пишется.
export const previewPersona = (id: number) => apiPost<PersonaPreview>(`/hotelier/questions/${id}/persona-preview`, {})
