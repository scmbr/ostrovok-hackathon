import { apiGet, apiPost } from '@/shared/api'
import type { CharacteristicDraft, Job } from '@/shared/api'

export const draftCharacteristic = (text: string) => apiPost<CharacteristicDraft>('/admin/characteristics/draft', { text })
export const saveCharacteristic = (yaml: string) => apiPost<{ key: string; title: string }>('/admin/characteristics', { yaml })
export const computeCharacteristic = (key: string) => apiPost<{ job_id: string }>(`/admin/characteristics/${encodeURIComponent(key)}/compute`, {})
export const getJob = (id: string) => apiGet<Job>(`/admin/jobs/${encodeURIComponent(id)}`)
