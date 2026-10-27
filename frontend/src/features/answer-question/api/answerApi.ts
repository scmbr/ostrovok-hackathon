import { apiPost } from '@/shared/api'
import type { AnswerOutcome, PersonaPreview } from '@/shared/api'

// comment — пояснение к кнопке: не разбирается, сохраняется рядом со свидетельством отельера.
export type AnswerBody = { text: string } | { answer: 'yes' | 'no'; comment?: string }

export const answerQuestion = (id: number, body: AnswerBody) => apiPost<AnswerOutcome>(`/hotelier/questions/${id}/answer`, body)

// Ответ имитированной персоны и его разбор; в БД ничего не пишется.
export const previewPersona = (id: number) => apiPost<PersonaPreview>(`/hotelier/questions/${id}/persona-preview`, {})
