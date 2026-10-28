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
    <section className="bg-hero">
      <div className="mx-auto flex max-w-3xl flex-col gap-6 px-4 pb-12 pt-16 md:pt-24">
        <h1 className="text-left font-display text-3xl font-bold leading-tight text-ink md:text-5xl">Брооонь без сюрпризов</h1>
        <ChatInput onSubmit={onSearch} submitLabel="Найти" footer={<StayDatesPicker />} />
        <ExampleChips onPick={onExample} />
      </div>
    </section>
  )
}
