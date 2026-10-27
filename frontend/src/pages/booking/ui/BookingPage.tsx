import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import type { Booking, HotelCardData } from '@/shared/api'
import { Button, Card, ErrorNote, Spinner } from '@/shared/ui'
import { ContractLineRow, WishStatusBadge } from '@/entities/booking'
import { getHotel } from '@/entities/hotel'
import { getBooking } from '@/features/book-with-wish'

// Заявка на бронь: оплаты нет. Статус пожелания читается из текущего ответа отеля.
export function BookingPage() {
  const id = Number(useParams().id)
  const [booking, setBooking] = useState<Booking | null>(null)
  const [hotel, setHotel] = useState<HotelCardData | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let live = true
    getBooking(id).then(
      (b) => {
        if (!live) return
        setBooking(b)
        getHotel(b.hotel_id).then(
          (h) => live && setHotel(h),
          () => undefined,
        )
      },
      (e: Error) => live && setError(e.message),
    )
    return () => {
      live = false
    }
  }, [id, tick])

  if (error) {
    return (
      <div className="mx-auto max-w-3xl p-4">
        <ErrorNote>{error}</ErrorNote>
      </div>
    )
  }
  if (!booking) {
    return (
      <div className="mx-auto max-w-3xl p-4">
        <Spinner label="Загружаю бронь…" />
      </div>
    )
  }

  const category = hotel?.rooms?.categories.find((c) => c.id === booking.category_id)?.name ?? booking.category
  return (
    <div className="mx-auto max-w-3xl space-y-4 px-4 py-4">
      <Link to="/" className="text-xs text-sky-800 hover:underline">
        ← к поиску
      </Link>
      <h1 className="text-2xl font-bold">Заявка на бронь № {booking.id}</h1>
      <Card>
        <p className="text-lg font-semibold">{hotel?.name ?? booking.hotel_name ?? `Отель ${booking.hotel_id}`}</p>
        <p className="text-sm text-slate-600">{category ?? `категория ${booking.category_id}`}</p>
        {booking.price_eur ? (
          <p className="mt-1 text-sm">
            {booking.price_eur} € за ночь
            {booking.price_mock && <span className="ml-1 text-xs text-slate-500">(цена из мок-карточки)</span>}
          </p>
        ) : null}
        <p className="mt-1 text-xs text-slate-500">Оплаты в прототипе нет: создана заявка.</p>
        {booking.wish && (
          <div className="mt-3 rounded-md bg-slate-50 p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <span>
                Пожелание: <b>{booking.wish.text}</b>
              </span>
              <WishStatusBadge status={booking.wish.status} />
              <Button small variant="ghost" className="ml-auto" onClick={() => setTick(tick + 1)}>
                Обновить статус
              </Button>
            </div>
            {booking.wish.status === 'waiting' && (
              <p className="mt-1 text-xs text-slate-600">Вопрос отправлен отелю; ответ придёт в кабинет отельера. Бронь это не блокирует.</p>
            )}
            {booking.wish.status === 'refused' && (
              <p className="mt-1 text-xs text-rose-700">Отель не гарантирует пожелание: номер выберут при заселении как обычно.</p>
            )}
          </div>
        )}
      </Card>
      <Card>
        <h2 className="font-semibold">Контракт ожиданий</h2>
        <ul className="mt-1 divide-y divide-slate-200">
          {booking.contract.map((l) => (
            <ContractLineRow key={l.key} line={l} />
          ))}
        </ul>
      </Card>
    </div>
  )
}
