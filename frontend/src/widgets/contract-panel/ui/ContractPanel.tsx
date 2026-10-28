import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { Contract } from '@/shared/api'
import { Button, ErrorNote, Modal, Spinner } from '@/shared/ui'
import { pct } from '@/shared/lib'
import { ContractLineRow } from '@/entities/booking'
import { categoryOffer, getHotel } from '@/entities/hotel'
import { AskHotelButton } from '@/features/ask-hotelier'
import { book, getContract } from '@/features/book-with-wish'
import type { ContractTarget } from '@/features/book-with-wish'
import { saveBookingStay, useStayDates } from '@/features/stay-dates'

// Контракт ожиданий перед бронью: что важно этому гостю, с уверенностью по каждому пункту.
// Неуверенное можно уточнить у отеля — бронь это не блокирует. Пожелание к номеру — только из предложенного системой.
export function ContractPanel({ target: given, onClose }: { target: ContractTarget; onClose: () => void }) {
  const navigate = useNavigate()
  const { stay } = useStayDates()
  const [withWish, setWithWish] = useState(false)
  const [contract, setContract] = useState<Contract | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [booking, setBooking] = useState(false)
  // Предложение пожелания: из выдачи или карточки; если вызывающий его не знает (сравнение), берём из размещения номеров отеля.
  const [looked, setLooked] = useState<ContractTarget['offer'] | null>(null)
  const target = useMemo<ContractTarget>(() => ({ ...given, offer: given.offer ?? looked ?? undefined }), [given, looked])

  useEffect(() => {
    if (given.offer) return
    let live = true
    getHotel(given.hotelId).then(
      (card) => live && setLooked(categoryOffer(card, given.categoryId)),
      () => undefined,
    )
    return () => {
      live = false
    }
  }, [given])

  useEffect(() => {
    let live = true
    setError(null)
    getContract(target, withWish).then(
      (c) => live && setContract(c),
      (e: Error) => live && setError(e.message),
    )
    return () => {
      live = false
    }
  }, [target, withWish])

  const submit = () => {
    setBooking(true)
    book(target, withWish)
      .then((b) => {
        // API брони дат не принимает: запоминаем выбранные для показа в подтверждении.
        saveBookingStay(b.id, stay)
        navigate(`/booking/${b.id}`)
      })
      .catch((e: Error) => {
        setError(e.message)
        setBooking(false)
      })
  }

  const offer = target.offer
  const refused = contract?.offer?.hotel_refused

  return (
    <Modal title={`Контракт ожиданий: ${target.categoryName}`} onClose={onClose}>
      {error && <ErrorNote>{error}</ErrorNote>}
      {!contract && !error && <Spinner label="Собираю контракт…" />}
      {contract && (
        <>
          <p className="text-sm text-ink">
            {contract.hotel_name} · {contract.category} ·{' '}
            <span title="Цена — мок: базовая карточка Островка заменена генератором">{contract.price_eur} € за ночь</span>
          </p>
          <ul className="mt-2 divide-y divide-line">
            {contract.lines.map((l) => (
              <ContractLineRow
                key={l.key}
                line={l}
                action={l.action === 'ask_hotel' ? <AskHotelButton hotelId={contract.hotel_id} charKey={l.key} /> : undefined}
              />
            ))}
          </ul>
          {offer && (
            <label className="mt-3 flex items-start gap-2 rounded-inner bg-brand-tint p-3 text-sm ring-1 ring-inset ring-brand">
              <input type="checkbox" className="mt-1" checked={withWish} disabled={!!refused} onChange={(e) => setWithWish(e.target.checked)} />
              <span>
                Добавить пожелание к брони: <b>{offer.text}</b>
                {offer.probability !== undefined && <> (вероятность ~{pct(offer.probability)})</>}
                <span className="block text-xs text-ink">
                  Отель получит один вопрос и подтвердит или нет. Бронь не блокируется, статус виден на странице брони.
                </span>
                {refused && <span className="block text-xs text-bad">Отель уже сообщил, что не гарантирует такие пожелания.</span>}
              </span>
            </label>
          )}
          <div className="mt-4 flex items-center gap-2">
            <Button variant="primary" disabled={booking} onClick={submit}>
              {booking ? 'Бронирую…' : 'Забронировать (заявка, без оплаты)'}
            </Button>
            <Button variant="ghost" onClick={onClose}>
              Закрыть
            </Button>
          </div>
        </>
      )}
    </Modal>
  )
}
