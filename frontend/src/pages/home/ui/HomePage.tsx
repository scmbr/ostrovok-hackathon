import { useNavigate, useLocation } from 'react-router-dom'
import type { DialogExample } from '@/shared/api'
import { HomeHero } from '@/widgets/home-hero'
import { HotelCarousel } from '@/widgets/hotel-carousel'
import type { HomeIntent, SearchEntryState } from '@/features/chat-search'

export function HomePage() {
  const navigate = useNavigate()
  const { search } = useLocation()
  const go = (intent: HomeIntent) => navigate({ pathname: '/search', search }, { state: { intent } satisfies SearchEntryState })
  return (
    <>
      <HomeHero onSearch={(message) => go({ message })} onExample={(example: DialogExample) => go({ example })} />
      <HotelCarousel />
    </>
  )
}
