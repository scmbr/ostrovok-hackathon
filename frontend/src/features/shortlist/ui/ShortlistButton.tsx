import { Button, Icon } from '@/shared/ui'
import { addHotel, removeHotel, useShortlist } from '../model/store'

export function ShortlistButton({ hotelId }: { hotelId: number }) {
  const { hotels } = useShortlist()
  const inList = hotels.some((h) => h.id === hotelId)
  return inList ? (
    <Button small className="!bg-brand-soft !text-brand-link" onClick={() => void removeHotel(hotelId)} title="Убрать из шортлиста">
      <Icon name="heart" className="fill-current" />в шортлисте
    </Button>
  ) : (
    <Button small onClick={() => void addHotel(hotelId)}>
      <Icon name="heart" />в шортлист
    </Button>
  )
}
