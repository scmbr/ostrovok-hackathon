import { Button } from '@/shared/ui'
import { addHotel, removeHotel, useShortlist } from '../model/store'

export function ShortlistButton({ hotelId }: { hotelId: number }) {
  const { hotels } = useShortlist()
  const inList = hotels.some((h) => h.id === hotelId)
  return inList ? (
    <Button small onClick={() => void removeHotel(hotelId)} title="Убрать из шортлиста">
      ★ в шортлисте
    </Button>
  ) : (
    <Button small onClick={() => void addHotel(hotelId)}>
      ☆ в шортлист
    </Button>
  )
}
