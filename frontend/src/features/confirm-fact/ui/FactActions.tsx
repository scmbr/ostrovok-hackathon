import { useState } from 'react'
import type { FactEditOutcome } from '@/shared/api'
import { Button, ErrorNote } from '@/shared/ui'
import { valueLabel } from '@/shared/lib'
import { confirmFact, correctFact } from '../api/factEditApi'
import type { FactAddress } from '../api/factEditApi'

interface Props {
  hotelId: number
  address: FactAddress
  // Допустимые значения для «исправить»; пусто — числа и структуры: значение берётся из расчёта.
  values: string[]
  onDone: (o: FactEditOutcome) => void
}

// «Подтвердить» и «исправить» значение факта: пишется свидетельство отельера, итог собирает движок правды.
export function FactActions({ hotelId, address, values, onDone }: Props) {
  const [busy, setBusy] = useState(false)
  const [correcting, setCorrecting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const run = async (fn: () => Promise<FactEditOutcome>) => {
    setBusy(true)
    setError(null)
    try {
      onDone(await fn())
      setCorrecting(false)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button small disabled={busy} onClick={() => run(() => confirmFact(hotelId, address))}>
          Подтвердить
        </Button>
        {values.length > 0 ? (
          <Button small variant="ghost" disabled={busy} onClick={() => setCorrecting(!correcting)}>
            Исправить
          </Button>
        ) : (
          <Button small variant="ghost" disabled={busy} onClick={() => setCorrecting(!correcting)}>
            Оспорить
          </Button>
        )}
      </div>
      {correcting && values.length > 0 && (
        <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
          <span>Как на самом деле:</span>
          {values.map((v) => (
            <Button key={v} small disabled={busy} onClick={() => run(() => correctFact(hotelId, address, v))}>
              {valueLabel(address.key, v === 'true' ? true : v === 'false' ? false : v)}
            </Button>
          ))}
        </div>
      )}
      {correcting && values.length === 0 && (
        <div className="mt-2 text-sm">
          <p className="text-xs text-slate-600">Числа не голосуют: значение берёт расчёт. Оспаривание ставит факт на пересчёт.</p>
          <Button small className="mt-1" disabled={busy} onClick={() => run(() => correctFact(hotelId, address, 'recheck'))}>
            Поставить на пересчёт
          </Button>
        </div>
      )}
      {error && <ErrorNote>{error}</ErrorNote>}
    </div>
  )
}
