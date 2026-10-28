// Даты проживания: чистые функции над датами вида YYYY-MM-DD (локальное время, без часовых поясов).
export interface Stay {
  checkin: string
  checkout: string
  nights: number
}

export const DEFAULT_LEAD_DAYS = 14
export const DEFAULT_NIGHTS = 3

const DAY_MS = 86_400_000

export function toIso(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function parseIso(s: string | null | undefined): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s ?? '')
  if (!m) return null
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return toIso(d) === s ? d : null
}

export function addDays(iso: string, days: number): string {
  const d = parseIso(iso) ?? new Date()
  d.setDate(d.getDate() + days)
  return toIso(d)
}

export function nightsBetween(checkin: string, checkout: string): number {
  const a = parseIso(checkin)
  const b = parseIso(checkout)
  return a && b ? Math.round((b.getTime() - a.getTime()) / DAY_MS) : 0
}

export function makeStay(checkin: string, checkout: string): Stay {
  return { checkin, checkout, nights: nightsBetween(checkin, checkout) }
}

export function defaultStay(now = new Date()): Stay {
  const checkin = addDays(toIso(now), DEFAULT_LEAD_DAYS)
  return makeStay(checkin, addDays(checkin, DEFAULT_NIGHTS))
}

// Допустимая пара дат: заезд не раньше сегодняшнего дня (если задан `from`), выезд позже заезда.
export function validStay(checkin: string | null | undefined, checkout: string | null | undefined, from: Date | null = new Date()): Stay | null {
  const a = parseIso(checkin)
  const b = parseIso(checkout)
  if (!a || !b || b <= a || (from && toIso(a) < toIso(from))) return null
  return makeStay(toIso(a), toIso(b))
}

export function nightsWord(n: number): string {
  const m10 = n % 10
  const m100 = n % 100
  if (m10 === 1 && m100 !== 11) return 'ночь'
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return 'ночи'
  return 'ночей'
}

export const formatDay = (iso: string): string => {
  const d = parseIso(iso)
  return d ? d.toLocaleDateString('ru-RU', { day: 'numeric', month: 'short' }) : iso
}

export const formatRange = (s: Pick<Stay, 'checkin' | 'checkout'>): string => `${formatDay(s.checkin)} — ${formatDay(s.checkout)}`

// Месяцы (1–12), в которые приходятся ночи проживания.
export function stayMonths(s: Pick<Stay, 'checkin' | 'checkout'>): number[] {
  const out: number[] = []
  const n = Math.max(1, nightsBetween(s.checkin, s.checkout))
  for (let i = 0; i < n; i++) {
    const d = parseIso(addDays(s.checkin, i))
    if (d && !out.includes(d.getMonth() + 1)) out.push(d.getMonth() + 1)
  }
  return out
}
