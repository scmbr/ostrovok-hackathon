import { useState } from 'react'
import type { SimilarHotel } from '@/shared/api'
import { Button, ErrorNote, Modal, Spinner } from '@/shared/ui'
import { SimilarList, getSimilar } from '@/entities/hotel'

// «Похожий на этот»: профиль собранных характеристик, без звёзд и цены. Считается без LLM.
export function FindSimilarButton({ hotelId, hotelName }: { hotelId: number; hotelName: string }) {
  const [open, setOpen] = useState(false)
  const [hotels, setHotels] = useState<SimilarHotel[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const show = () => {
    setOpen(true)
    if (hotels) return
    getSimilar(hotelId).then(setHotels, (e: Error) => setError(e.message))
  }
  return (
    <>
      <Button small onClick={show}>
        Похожий
      </Button>
      {open && (
        <Modal title={`Похожие на «${hotelName}»`} onClose={() => setOpen(false)}>
          {error && <ErrorNote>{error}</ErrorNote>}
          {!error && !hotels && <Spinner label="Ищу похожие…" />}
          {hotels && <SimilarList hotels={hotels} />}
        </Modal>
      )}
    </>
  )
}
