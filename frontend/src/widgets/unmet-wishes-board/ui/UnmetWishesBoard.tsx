import { useEffect, useState } from 'react'
import { apiGet } from '@/shared/api'
import { Button, Card, ErrorNote, Spinner } from '@/shared/ui'
import { formatDate } from '@/shared/lib'

interface Proposal {
  key?: string
  title?: string
  how?: string
}

interface UnmetWish {
  text: string
  normalized: string
  count: number
  first_seen: string
  last_seen: string
  proposal: Proposal | null
}

interface Props {
  // Передать описание в форму добавления характеристики текстом; сама ничего не создаёт.
  onPropose: (description: string) => void
}

// Описание для формы: предложение модели, если оно есть, иначе формулировка гостя.
function description(w: UnmetWish): string {
  const p = w.proposal
  return (p?.how || p?.title || '').trim() || w.text
}

// Журнал неизвестных пожеланий: чего гости ищут, а в реестре этого нет. Частые — кандидаты в новые характеристики.
export function UnmetWishesBoard({ onPropose }: Props) {
  const [wishes, setWishes] = useState<UnmetWish[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    apiGet<{ wishes: UnmetWish[] }>('/admin/unmet-wishes').then((r) => setWishes(r.wishes), (e: Error) => setError(e.message))
  }, [])

  if (error) return <ErrorNote>{error}</ErrorNote>
  if (!wishes) return <Spinner label="Загружаю журнал пожеланий…" />
  return (
    <Card>
      <h3 className="font-semibold">Неизвестные пожелания</h3>
      <p className="mb-2 text-xs text-slate-500">
        Желания гостей, которых нет в реестре. Само в реестр ничего не попадает: характеристику добавляет человек, кнопка только открывает форму с описанием.
      </p>
      {wishes.length === 0 ? (
        <p className="text-sm text-slate-600">Пока пусто: гости не просили ничего, чего нет в реестре.</p>
      ) : (
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-slate-200 text-left text-xs text-slate-500">
              <th className="py-1 pr-2">Пожелание</th>
              <th className="py-1 pr-2 text-right">Раз</th>
              <th className="py-1 pr-2">Впервые / в последний раз</th>
              <th className="py-1" />
            </tr>
          </thead>
          <tbody>
            {wishes.map((w) => (
              <tr key={w.normalized} className="border-b border-slate-100 align-top">
                <td className="py-1.5 pr-2">
                  <div>{w.text}</div>
                  <div className="text-xs text-slate-500">
                    {w.normalized}
                    {w.proposal?.key ? ` · предложено: ${w.proposal.key}` : ''}
                  </div>
                </td>
                <td className="py-1.5 pr-2 text-right font-semibold">{w.count}</td>
                <td className="py-1.5 pr-2 text-xs text-slate-600">
                  {formatDate(w.first_seen)} / {formatDate(w.last_seen)}
                </td>
                <td className="py-1.5 text-right">
                  <Button onClick={() => onPropose(description(w))}>Предложить характеристику</Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Card>
  )
}
