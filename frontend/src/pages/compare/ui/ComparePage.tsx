import { useEffect, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import type { Compare, CompareSide } from '@/shared/api'
import { ErrorNote, Spinner } from '@/shared/ui'
import { CompareView } from '@/widgets/compare-view'
import { ContractPanel } from '@/widgets/contract-panel'
import { lastSearch, useChat } from '@/features/chat-search'
import { compare } from '@/features/compare-hotels'
import type { ContractTarget } from '@/features/book-with-wish'

const ids = (s: string | null) =>
  (s ?? '')
    .split(',')
    .map(Number)
    .filter((n) => Number.isInteger(n) && n > 0)

// /compare?hotels=161,188 — лучшие варианты отелей; /compare?hotel=161&cats=1,2 — категории одного отеля.
export function ComparePage() {
  const [params] = useSearchParams()
  const chat = useChat()
  const wishes = lastSearch(chat)?.query.wishes
  const hotels = params.get('hotels')
  const hotel = Number(params.get('hotel')) || 0
  const cats = params.get('cats')
  const [data, setData] = useState<Compare | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [target, setTarget] = useState<ContractTarget | null>(null)

  useEffect(() => {
    let live = true
    setData(null)
    setError(null)
    compare(hotel ? { wishes, hotelId: hotel, categoryIds: ids(cats) } : { wishes, hotelIds: ids(hotels) }).then(
      (c) => live && setData(c),
      (e: Error) => live && setError(e.message),
    )
    return () => {
      live = false
    }
    // желания берутся один раз при открытии страницы
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hotels, hotel, cats])

  const book = (s: CompareSide) => setTarget({ hotelId: s.hotel_id, categoryId: s.category_id, categoryName: s.category, wishes })

  return (
    <div className="mx-auto max-w-5xl space-y-4 px-4 py-4">
      <Link to="/" className="text-xs text-sky-800 hover:underline">
        ← к поиску
      </Link>
      <h1 className="text-2xl font-bold">{hotel ? 'Сравнение номеров' : 'Сравнение отелей'}</h1>
      {error && (
        <ErrorNote>
          {error}
          {error.includes('нет желаний') && (
            <>
              {' '}
              <Link to="/" className="underline">
                Сначала опишите, что вам важно, в чате
              </Link>
              : сравнение строится относительно ваших желаний.
            </>
          )}
        </ErrorNote>
      )}
      {!data && !error && <Spinner label="Сравниваю…" />}
      {data && <CompareView data={data} onBook={book} />}
      {target && <ContractPanel target={target} onClose={() => setTarget(null)} />}
    </div>
  )
}
