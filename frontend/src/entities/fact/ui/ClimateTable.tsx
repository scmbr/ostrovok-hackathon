import { Icon } from '@/shared/ui'
import { formatRange, stayMonths } from '@/shared/lib'

// Месячные нормы климата из значения факта climate_by_month (value.months, value.period).
export interface ClimateMonth {
  month: number
  air_mean_c: number
  air_min_c?: number
  air_max_c?: number
  water_c: number | null
  precip_mm: number
  swimming: boolean
}

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'май', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']

export function climateMonths(value: Record<string, unknown>): ClimateMonth[] | null {
  const m = value.months
  return Array.isArray(m) && m.length > 0 && typeof m[0] === 'object' ? (m as ClimateMonth[]) : null
}

const num = (v: number | null | undefined) => (v === null || v === undefined ? '—' : String(v).replace('.', ','))

export interface StayRange {
  checkin: string
  checkout: string
}

const range = (xs: number[]) => {
  const lo = Math.round(Math.min(...xs))
  const hi = Math.round(Math.max(...xs))
  return lo === hi ? `${lo}` : `${lo}–${hi}`
}

// Строка «в ваши даты»: по месяцам, на которые приходятся ночи проживания.
function stayLine(months: ClimateMonth[], stay: StayRange): string {
  const marked = stayMonths(stay)
  const own = months.filter((m) => marked.includes(m.month))
  if (!own.length) return ''
  const water = own.map((m) => m.water_c).filter((w): w is number => w !== null)
  const swim = own.every((m) => m.swimming) ? 'да' : own.some((m) => m.swimming) ? 'в части дат' : 'нет'
  return `в ваши даты (${formatRange(stay)}): воздух ${range(own.map((m) => m.air_mean_c))} °C, вода ${water.length ? `${range(water)} °C` : 'данных нет'}, купание: ${swim}`
}

export function ClimateTable({ value, stay }: { value: Record<string, unknown>; stay?: StayRange }) {
  const months = climateMonths(value)
  if (!months) return null
  const period = value.period as { from_year?: number; to_year?: number } | undefined
  const marked = stay ? stayMonths(stay) : [new Date().getMonth() + 1]
  const line = stay ? stayLine(months, stay) : ''
  return (
    <div className="mt-2">
      {line && <p className="mb-1 text-caption font-medium text-brand-link">{line}</p>}
      <div className="overflow-x-auto rounded-inner bg-surface-muted p-3">
        <table className="w-full text-caption">
          <thead>
            <tr className="text-ink-muted">
              <th className="pb-1 pr-3 text-left font-medium">Месяц</th>
              <th className="pb-1 pr-3 text-right font-medium">Воздух, °C</th>
              <th className="pb-1 pr-3 text-right font-medium">Вода, °C</th>
              <th className="pb-1 pr-3 text-right font-medium">Осадки, мм</th>
              <th className="pb-1 text-center font-medium">Купание</th>
            </tr>
          </thead>
          <tbody>
            {months.map((m) => (
              <tr key={m.month} className={`border-t border-line ${marked.includes(m.month) ? 'font-medium text-brand-link' : 'text-ink'}`}>
                <td className="py-0.5 pr-3">{MONTHS[m.month - 1] ?? m.month}</td>
                <td className="py-0.5 pr-3 text-right" title={m.air_min_c !== undefined ? `от ${num(m.air_min_c)} до ${num(m.air_max_c)}` : undefined}>
                  {num(m.air_mean_c)}
                </td>
                <td className="py-0.5 pr-3 text-right">{num(m.water_c)}</td>
                <td className="py-0.5 pr-3 text-right">{num(m.precip_mm)}</td>
                <td className="py-0.5 text-center">
                  {m.swimming ? <Icon name="check" className="inline text-ok" /> : <span className="text-ink-muted">—</span>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-1 text-caption text-ink-muted">
        Средние по месяцам{period?.from_year ? `, ${period.from_year}–${period.to_year}` : ''}. Купание — вода от 20 °C; «—» у воды: данных нет.
      </p>
    </div>
  )
}
