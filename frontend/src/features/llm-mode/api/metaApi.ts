import { apiGet } from '@/shared/api'
import type { Meta } from '@/shared/api'

export const getMeta = () => apiGet<Meta>('/meta')
