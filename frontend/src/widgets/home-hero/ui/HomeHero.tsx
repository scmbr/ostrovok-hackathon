import type { DialogExample } from '@/shared/api'
import { ChatInput, ExampleChips } from '@/features/chat-search'
import { StayDatesPicker } from '@/features/stay-dates'

interface Props {
  // Поиск начинается на странице выше: виджет только собирает поле, даты и примеры.
  onSearch: (message: string) => void
  onExample: (ex: DialogExample) => void
}

export function HomeHero({ onSearch, onExample }: Props) {
  return (
    <section className="mx-auto flex max-w-3xl flex-col items-center gap-6 px-4 pb-8 pt-16 text-center md:pt-24">
      <h1 className="font-display text-3xl font-bold leading-tight text-ink md:text-5xl">Брооонь без сюрпризов</h1>
      <div className="w-full text-left">
        <ChatInput onSubmit={onSearch} submitLabel="Найти" footer={<StayDatesPicker />} />
      </div>
      <ExampleChips onPick={onExample} />
    </section>
  )
}
