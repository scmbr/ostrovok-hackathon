// Фото: свой файл, иначе запасной блок (как у иконок). Список файлов берём из attribution.json,
// который лежит в src (из public JSON импортировать нельзя): если файла нет, glob возвращает {} и сборка не падает.
// Сами картинки через glob не импортируем: из public их нельзя бандлить, они отдаются как есть.
interface Credit {
  author: string
  license: string
  source: string
  file: string
}
interface Manifest {
  hotels: Record<string, Credit[]>
  rooms: Record<string, Credit>
}

const FILES = import.meta.glob('./attribution.json', { eager: true, import: 'default' }) as Record<
  string,
  Manifest
>
const MANIFEST: Manifest = Object.values(FILES)[0] ?? { hotels: {}, rooms: {} }

export interface HotelPhotoData {
  src: string
  credit: string
  source: string
}

// Фото отеля с подписью «фото: автор, лицензия» (Wikimedia Commons).
export function hotelPhotos(hotelId: number): HotelPhotoData[] {
  return (MANIFEST.hotels[String(hotelId)] ?? []).map((c) => ({
    src: `/hotels/${hotelId}/${c.file}`,
    credit: `фото: ${c.author}, ${c.license}`,
    source: c.source,
  }))
}

export function hasHotelPhoto(hotelId: number): boolean {
  return (MANIFEST.hotels[String(hotelId)]?.length ?? 0) > 0
}

// Нейтральная иллюстрация типа номера (без вида из окна); неизвестный тип — запасной.
const ROOM_ALIAS: Record<string, string> = {
  'sea view': 'superior',
  'vista mar': 'superior',
  family: 'standard',
}
export function roomPhoto(category: string): HotelPhotoData | null {
  const key = category.trim().toLowerCase()
  const name = MANIFEST.rooms[key] ? key : (ROOM_ALIAS[key] ?? 'standard')
  const c = MANIFEST.rooms[name]
  if (!c) return null
  return { src: `/rooms/${c.file}`, credit: `иллюстрация, фото: ${c.author}, ${c.license}`, source: c.source }
}
