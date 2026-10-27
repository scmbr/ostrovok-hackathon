import { apiGet } from '@/shared/api'
import type { DialogExample } from '@/shared/api'

export const getExamples = () => apiGet<{ examples: DialogExample[] }>('/dialog/examples').then((r) => r.examples)
