import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import type { HotelCardData } from '@/shared/api'
import { Card, ErrorNote, Spinner } from '@/shared/ui'
import { ContractPanel } from '@/widgets/contract-panel'
import { HotelFacts } from '@/widgets/hotel-facts'
import { FacadeMap } from '@/widgets/hotel-map'
import { getHotel, HotelGallery } from '@/entities/hotel'
import { lastSearch, useChat } from '@/features/chat-search'
import { FindSimilarButton } from '@/features/find-similar'
import { ShortlistButton } from '@/features/shortlist'
import type { ContractTarget } from '@/features/book-with-wish'

export function HotelPage() {
  const id = Number(useParams().id)
  const chat = useChat()
  const [hotel, setHotel] = useState<HotelCardData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [target, setTarget] = useState<ContractTarget | null>(null)
  const wishes = lastSearch(chat)?.query.wishes

  useEffect(() => {
    setHotel(null)
    setError(null)
    getHotel(id).then(setHotel, (e: Error) => setError(e.message))
  }, [id])

  if (error) {
    return (
      <div className="mx-auto max-w-5xl p-4">
        <ErrorNote>{error}</ErrorNote>
      </div>
    )
  }
  if (!hotel) {
    return (
      <div className="mx-auto max-w-5xl p-4">
        <Spinner label="Загружаю отель…" />
      </div>
    )
  }

  return (
    <div className="mx-auto max-w-5xl space-y-5 px-4 py-4">
      <div className="flex flex-wrap items-start gap-3">
        <div>
          <Link to="/search" className="text-xs text-brand-link hover:underline">
            ← к поиску
          </Link>
          <h1 className="text-2xl font-bold">{hotel.name}</h1>
          <p className="text-sm text-ink">
            {hotel.district} · {'★'.repeat(hotel.card.stars)}
            {hotel.card.mock && ' · базовая карточка (звёзды, цена, завтрак) — мок'}
          </p>
        </div>
        <div className="ml-auto flex gap-2">
          <ShortlistButton hotelId={hotel.id} />
          <FindSimilarButton hotelId={hotel.id} hotelName={hotel.name} />
        </div>
      </div>
      <HotelGallery hotelId={hotel.id} name={hotel.name} />
      {hotel.rooms && hotel.rooms.facades.length > 0 && (
        <Card>
          <h2 className="mb-2 text-lg font-semibold">Фасады</h2>
          <FacadeMap hotel={hotel} />
        </Card>
      )}
      <HotelFacts hotel={hotel} wishes={wishes} onBook={setTarget} />
      {target && <ContractPanel target={target} onClose={() => setTarget(null)} />}
    </div>
  )
}
