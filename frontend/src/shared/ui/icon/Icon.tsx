import { FALLBACK, type IconName } from './map'

// Свои иконки: custom/<name>.svg. Если папка пуста, glob возвращает {} и работает запасной lucide.
const CUSTOM = import.meta.glob('./custom/*.svg', { eager: true, query: '?raw', import: 'default' }) as Record<string, string>

// Файл лежит в репозитории, не приходит от пользователя; всё равно вырезаем скрипты, стили, обработчики
// и убираем width/height у корневого <svg>: размер задаёт компонент.
function sanitize(svg: string): string {
  return svg
    .replace(/<script[\s\S]*?<\/script>/gi, '')
    .replace(/<style[\s\S]*?<\/style>/gi, '')
    .replace(/\son\w+="[^"]*"/gi, '')
    .replace(/<svg[^>]*>/i, (tag) => tag.replace(/\s(width|height)="[^"]*"/gi, ''))
}

export function Icon({ name, size = 16, className = '' }: { name: IconName; size?: number; className?: string }) {
  const raw = CUSTOM[`./custom/${name}.svg`]
  if (raw) {
    return (
      <span
        aria-hidden="true"
        className={`inline-flex shrink-0 [&>svg]:h-full [&>svg]:w-full ${className}`}
        style={{ width: size, height: size }}
        dangerouslySetInnerHTML={{ __html: sanitize(raw) }}
      />
    )
  }
  const Fallback = FALLBACK[name]
  return <Fallback aria-hidden="true" size={size} strokeWidth={2} color="currentColor" className={`shrink-0 ${className}`} />
}

export type { IconName }
