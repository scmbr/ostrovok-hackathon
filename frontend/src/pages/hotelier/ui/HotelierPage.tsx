import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Badge } from '@/shared/ui'
import { personaLabel } from '@/shared/lib'
import { HotelierFacts } from '@/widgets/hotelier-facts'
import { HotelierInbox } from '@/widgets/hotelier-inbox'
import { getHotelierHotels } from '@/entities/question'
import { HotelPicker, useRole } from '@/features/switch-role'

// Кабинет отельера: вопросы системы, результат ответа и «что система знает об отеле». Без авторизации: отель выбирается.
export function HotelierPage() {
  const { hotelId } = useRole()
  const [persona, setPersona] = useState('')
  const [name, setName] = useState('')
  useEffect(() => {
    getHotelierHotels().then(
      (hs) => {
        const h = hs.find((x) => x.id === hotelId)
        setPersona(h?.persona ?? '')
        setName(h?.name ?? '')
      },
      () => undefined,
    )
  }, [hotelId])

  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 py-4">
      <div>
        <Link to="/" className="text-xs text-sky-800 hover:underline">
          ← к поиску
        </Link>
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-2xl font-bold">Кабинет отельера{name ? `: ${name}` : ` (отель ${hotelId})`}</h1>
          {persona && <Badge tone="violet">имитация: {personaLabel(persona)}</Badge>}
        </div>
        <div className="mt-2">
          <HotelPicker hotelId={hotelId} />
        </div>
      </div>
      <HotelierInbox hotelId={hotelId} hasPersona={!!persona} />
      <HotelierFacts hotelId={hotelId} />
    </div>
  )
}
