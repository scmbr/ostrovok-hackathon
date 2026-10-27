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

// Свидетельство человека с нулевым вкладом: после поправок (репутация, асимметрия, свежесть) его надёжность не выше 50 %
// и информации не несёт, чаще всего из-за упавшего доверия. reliability в ответе API — база, а не итог поправок.
// Показываем причину, а не просто упавшую уверенность факта.
export function zeroWeightNote(e: { author_type: string; contribution: number; reliability: number; author_trust?: number }): string | null {
  if (e.author_type === 'tool' || e.contribution !== 0) return null
  const who = e.author_type === 'hotelier' ? 'утверждениям отеля в его пользу' : 'этому автору'
  const trust = e.author_trust !== undefined ? ` (репутация ${e.author_trust.toFixed(2)})` : ''
  return `вес ≈ 0: мало доверия к ${who}${trust}`
}
