import { useState } from 'react'
import { Badge, Button } from '@/shared/ui'
import { askHotel, useAsked } from '../model/asked'

// «Уточнить у отеля» → «спросили отель». Вопрос про характеристику отеля, ответ получат все будущие гости.
export function AskHotelButton({ hotelId, charKey }: { hotelId: number; charKey: string }) {
  const asked = useAsked(hotelId, charKey)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  if (asked) {
    return (
      <Badge tone="violet" title={asked.message}>
        спросили отель
      </Badge>
    )
  }
  const click = () => {
    setBusy(true)
    setError(null)
    askHotel(hotelId, charKey)
      .catch((e: Error) => setError(e.message))
      .finally(() => setBusy(false))
  }
  return (
    <span className="inline-flex items-center gap-2">
      <Button small disabled={busy} onClick={click}>
        Уточнить у отеля
      </Button>
      {error && <span className="text-xs text-rose-700">{error}</span>}
    </span>
  )
}
