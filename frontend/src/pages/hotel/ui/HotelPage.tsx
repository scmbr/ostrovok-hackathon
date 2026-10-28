import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import type { HotelCardData } from '@/shared/api'
import { Card, ErrorNote, Spinner } from '@/shared/ui'
import { ContractPanel } from '@/widgets/contract-panel'
import { HotelFacts } from '@/widgets/hotel-facts'
import { HotelHeader } from '@/widgets/hotel-header'
import { FacadeMap } from '@/widgets/hotel-map'
import { getHotel, HotelGallery } from '@/entities/hotel'
import { lastSearch, useChat } from '@/features/chat-search'
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

  const search = lastSearch(chat)
  const best = search?.hotels.find((h) => h.hotel_id === hotel.id)?.best

  return (
    <div className="mx-auto max-w-5xl space-y-5 px-4 py-4">
      <HotelHeader hotel={hotel} wishes={wishes} bestCategoryId={best?.category_id} onBook={setTarget} />
      <HotelGallery hotelId={hotel.id} name={hotel.name} />
      {hotel.rooms && hotel.rooms.facades.length > 0 && (
        <Card>
          <h2 className="mb-2 text-lg font-semibold">Фасады</h2>
          <FacadeMap hotel={hotel} />
        </Card>
      )}
      <HotelFacts hotel={hotel} wishes={wishes} evals={best?.wishes} onBook={setTarget} />
      {target && <ContractPanel target={target} onClose={() => setTarget(null)} />}
    </div>
  )
}
