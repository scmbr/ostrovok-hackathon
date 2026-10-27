import { useEffect } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { AppHeader } from '@/widgets/app-header'
import { AdminPage } from '@/pages/admin'
import { BookingPage } from '@/pages/booking'
import { ComparePage } from '@/pages/compare'
import { HotelPage } from '@/pages/hotel'
import { HotelierPage } from '@/pages/hotelier'
import { SearchPage } from '@/pages/search'
import { checkSessionExpiry } from '@/entities/session'

export function App() {
  useEffect(() => {
    checkSessionExpiry()
  }, [])
  return (
    <BrowserRouter>
      <AppHeader />
      <main>
        <Routes>
          <Route path="/" element={<SearchPage />} />
          <Route path="/hotel/:id" element={<HotelPage />} />
          <Route path="/compare" element={<ComparePage />} />
          <Route path="/booking/:id" element={<BookingPage />} />
          <Route path="/hotelier" element={<HotelierPage />} />
          <Route path="/admin" element={<AdminPage />} />
          <Route path="*" element={<SearchPage />} />
        </Routes>
      </main>
    </BrowserRouter>
  )
}
