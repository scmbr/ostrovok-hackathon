import { Link } from 'react-router-dom'
import { LlmModeBadge, LlmModeBanner } from '@/features/llm-mode'
import { RoleSwitch } from '@/features/switch-role'

export function AppHeader() {
  return (
    <header className="sticky top-0 z-[1000] border-b border-slate-200 bg-white">
      <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-3 px-4 py-2">
        <Link to="/" className="text-lg font-bold text-sky-900">
          Бронь без сюрпризов
        </Link>
        <span className="hidden text-xs text-slate-500 md:inline">Барселона · хакатон Островка</span>
        <div className="ml-auto flex flex-wrap items-center gap-3">
          <LlmModeBadge />
          <RoleSwitch />
        </div>
      </div>
      <LlmModeBanner />
    </header>
  )
}
