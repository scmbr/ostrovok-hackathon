import { useNavigate } from 'react-router-dom'
import { DEFAULT_HOTELIER_HOTEL, roleHref, useRole } from '../model/useRole'
import type { Role } from '../model/useRole'

const LABELS: Record<Role, string> = { guest: 'Гость', hotelier: 'Отельер', admin: 'Админ' }

export function RoleSwitch() {
  const { role, hotelId } = useRole()
  const navigate = useNavigate()
  return (
    <div className="flex items-center gap-1" role="group" aria-label="Роль">
      {(Object.keys(LABELS) as Role[]).map((r) => (
        <button
          key={r}
          type="button"
          onClick={() => navigate(roleHref(r, r === 'hotelier' ? hotelId || DEFAULT_HOTELIER_HOTEL : undefined))}
          className={`h-[34px] rounded-inner px-3 text-body font-medium text-brand-link hover:bg-brand-tint ${r === role ? 'bg-brand-tint' : ''}`}
        >
          {LABELS[r]}
          {r === 'hotelier' && role === 'hotelier' ? ` ${hotelId}` : ''}
        </button>
      ))}
    </div>
  )
}
