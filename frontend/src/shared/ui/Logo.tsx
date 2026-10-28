import { useState } from 'react'

// Свой /brand/logo.svg, иначе текстовый знак.
export function Logo() {
  const [failed, setFailed] = useState(false)
  if (!failed) {
    return <img src="/brand/logo.svg" alt="Бронь без сюрпризов" className="h-6" onError={() => setFailed(true)} />
  }
  return (
    <span className="inline-flex items-center gap-2 text-title text-ink">
      <span className="h-5 w-5 rounded-[6px] bg-grad-brand" aria-hidden="true" />
      Бронь без сюрпризов
    </span>
  )
}
