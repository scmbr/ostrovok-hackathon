import { useState } from 'react'
import { hotelPhotos, roomPhoto, type HotelPhotoData } from '../lib/photos'

// Без файла (или если он не загрузился) — блок с градиентом, названием и «фото не найдено».
function Placeholder({ name, className }: { name: string; className: string }) {
  return (
    <div className={`flex flex-col items-center justify-center gap-1 bg-grad-brand p-2 text-center text-white ${className}`}>
      <span className="text-caption font-bold leading-tight">{name}</span>
      <span className="text-caption opacity-80">фото не найдено</span>
    </div>
  )
}

function Img({ photo, alt, className }: { photo: HotelPhotoData; alt: string; className: string }) {
  return <img src={photo.src} alt={alt} loading="lazy" className={`object-cover ${className}`} />
}

// Миниатюра отеля слева в карточке выдачи.
export function HotelThumb({ hotelId, name }: { hotelId: number; name: string }) {
  const photo = hotelPhotos(hotelId)[0]
  const [broken, setBroken] = useState(false)
  const box = 'h-28 w-36 shrink-0 rounded-inner'
  if (!photo || broken) return <Placeholder name={name} className={box} />
  return (
    <div className="shrink-0">
      <img
        src={photo.src}
        alt={name}
        loading="lazy"
        onError={() => setBroken(true)}
        className={`${box} object-cover`}
        title={photo.credit}
      />
    </div>
  )
}

// Галерея на карточке отеля: до двух фото, под каждым подпись об авторе и лицензии.
export function HotelGallery({ hotelId, name }: { hotelId: number; name: string }) {
  const photos = hotelPhotos(hotelId)
  if (photos.length === 0) return <Placeholder name={name} className="h-48 w-full rounded-inner" />
  return (
    <div className="flex flex-wrap gap-2">
      {photos.map((p) => (
        <figure key={p.src} className="min-w-0 flex-1 basis-64">
          <Img photo={p} alt={name} className="h-56 w-full rounded-inner" />
          <figcaption className="mt-1 text-caption text-ink-muted">
            <a href={p.source} target="_blank" rel="noreferrer" className="hover:underline">
              {p.credit}
            </a>
          </figcaption>
        </figure>
      ))}
    </div>
  )
}

// Иллюстрация типа номера (не фото конкретного номера).
export function RoomThumb({ category }: { category: string }) {
  const photo = roomPhoto(category)
  if (!photo) return null
  return (
    <figure className="shrink-0" title={photo.credit}>
      <Img photo={photo} alt={`Иллюстрация: ${category}`} className="h-14 w-20 rounded-sm" />
      <figcaption className="text-center text-caption text-ink-muted">иллюстрация</figcaption>
    </figure>
  )
}
