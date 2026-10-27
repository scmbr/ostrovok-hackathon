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

// Значение факта для показа: поле значения называет реестр (value_field), иначе берём единственное поле.
export function showValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (typeof v === 'boolean') return v ? 'да' : 'нет'
  if (typeof v === 'object') {
    const entries = Object.entries(v as Record<string, unknown>).filter(([, x]) => typeof x !== 'object')
    return entries.map(([k, x]) => `${k}: ${showValue(x)}`).join(', ')
  }
  return typeof v === 'string' ? (ENUM_RU[v] ?? v) : String(v)
}

const ENUM_RU: Record<string, string> = {
  quiet: 'тихо',
  moderate: 'умеренно',
  loud: 'шумно',
  rich: 'много',
  basic: 'базово',
  none: 'нет',
  full: 'есть',
}

// Основное значение факта или утверждения: поле значения из реестра, а если его нет — первое известное поле.
// Остальное (параметры расчёта) лежит в свидетельствах и здесь только мешает.
const MAIN_FIELDS = ['view', 'level', 'available', 'minutes', 'min_floor']

export function mainValue(v: Record<string, unknown>, field?: string): string {
  const key = field && field in v ? field : MAIN_FIELDS.find((k) => k in v)
  if (!key) return Object.values(v).some((x) => typeof x !== 'object') ? showValue(v) : 'по месяцам, см. источники'
  const x = v[key]
  if (x === null || x === undefined) return key === 'min_floor' ? 'моря не видно' : '—'
  if (key === 'minutes') return `${x} мин`
  if (key === 'min_floor') return `с ${x}-го этажа`
  return typeof x === 'string' ? (ENUM_RU[x] ?? x) : showValue(x)
}
