import { summaryLabel, valueLabel } from './labels'

export const pct = (p: number) => `${Math.round(p * 100)} %`

export function formatDate(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short', year: 'numeric' })
}

const SOURCE_LABELS: Record<string, string> = {
  computed: 'расчёт',
  hotelier: 'отельер',
  guest: 'гости',
  guests: 'гости',
  catastro: 'кадастр',
  osm: 'OpenStreetMap',
  bcn_noise: 'карта шума BCN',
  open_meteo: 'Open-Meteo',
}

export const sourceLabel = (s: string) => SOURCE_LABELS[s] ?? s

const AUTHOR_LABELS: Record<string, string> = { tool: 'расчёт', hotelier: 'отельер', guest: 'гость' }
export const authorLabel = (a: string) => AUTHOR_LABELS[a] ?? a

// Основное значение факта или утверждения: поле значения из реестра, а если его нет — первое известное поле.
// Остальное (параметры расчёта) лежит в свидетельствах и здесь только мешает. Подписи — shared/lib/labels.json.
const MAIN_FIELDS = ['view', 'level', 'available', 'minutes', 'min_floor']

export function mainValue(v: Record<string, unknown>, field?: string, key?: string): string {
  const f = field && field in v ? field : MAIN_FIELDS.find((k) => k in v)
  if (!f) return Object.values(v).some((x) => typeof x !== 'object') ? valueLabel(key, undefined) : summaryLabel(key)
  return valueLabel(key, v[f])
}
