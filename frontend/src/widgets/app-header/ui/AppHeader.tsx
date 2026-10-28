import { Link } from 'react-router-dom'
import { LlmModeBadge, LlmModeBanner } from '@/features/llm-mode'
import { RoleSwitch, useRole } from '@/features/switch-role'
import { Logo } from '@/shared/ui'

export function AppHeader() {
  const { role } = useRole()
  return (
    <header className="sticky top-0 z-[1000] border-b border-surface-muted bg-surface">
      <div className="mx-auto flex h-16 max-w-7xl items-center gap-3 px-4">
        <Link to="/" className="flex items-center">
          <Logo />
        </Link>
        <span className="hidden text-caption text-ink-muted md:inline">Барселона · хакатон Островка</span>
        <div className="ml-auto flex items-center gap-3">
          <LlmModeBadge />
          <RoleSwitch />
        </div>
      </div>
      <LlmModeBanner />
    </header>
  )
}
