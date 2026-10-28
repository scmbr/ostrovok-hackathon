import labels from './labels.json'

// Подписи значений характеристик, статусов контракта и брони. Живут на фронте, а не в реестре: поле подписей в реестре
// попало бы в list_characteristics агентов и сломало их кеши. Полноту относительно реестра проверяет тест бэкенда
// (internal/adapter/http/labels_test.go): новая характеристика без подписи не проходит.

interface CharLabels {
  values?: Record<string, string>
  unit?: string // шаблон с {n}
  empty?: string // подпись пустого значения числа
  summary?: string // составное значение (по месяцам)
}

const chars = labels.characteristics as Record<string, CharLabels>
const warned = new Set<string>()

// Незнакомое значение показываем как есть; в dev-сборке предупреждаем один раз.
function unknownValue(key: string | undefined, v: unknown): string {
  const id = `${key}:${String(v)}`
  if (import.meta.env.DEV && !warned.has(id)) {
    warned.add(id)
    console.warn(`нет подписи значения «${String(v)}» у характеристики «${key ?? '?'}» (shared/lib/labels.json)`)
  }
  return String(v)
}

export function valueLabel(key: string | undefined, v: unknown): string {
  if (v === null || v === undefined) return (key && chars[key]?.empty) || '—'
  if (typeof v === 'boolean') return labels.bool[v ? 'true' : 'false']
  const c = key ? chars[key] : undefined
  if (typeof v === 'number') return c?.unit ? c.unit.replace('{n}', String(v)) : String(v)
  if (typeof v === 'string') return c?.values?.[v] ?? unknownValue(key, v)
  // Массив или объект (составное значение, например по месяцам): String() даёт [object Object].
  if (typeof v === 'object') return summaryLabel(key)
  return String(v)
}

export const summaryLabel = (key?: string) => (key && chars[key]?.summary) || 'см. источники'

export const contractLabel = (kind: string) => (labels.contract as Record<string, string>)[kind] ?? unknownValue('contract', kind)
export const bookingWishLabel = (status: string) => (labels.booking_wish as Record<string, string>)[status] ?? unknownValue('booking_wish', status)

const group = (name: string, key: string): string => ((labels as unknown as Record<string, Record<string, string>>)[name] ?? {})[key] ?? key

export const personaLabel = (p: string) => group('persona', p)
export const reasonKindLabel = (k: string) => group('reason_kind', k)
export const questionStatusLabel = (s: string) => group('question_status', s)
export const parsedByLabel = (m: string) => group('parsed_by', m)
export const conflictChangeLabel = (c: string) => group('conflict_change', c)
export const resolutionKindLabel = (k: string) => group('resolution_kind', k)
export const evidenceRoleLabel = (r: string) => group('evidence_role', r)

// Есть ли подписи значений у характеристики: у добавленных текстом их нет, значение показывается как есть.
export const hasLabels = (key: string) => key in chars
