import { useNavigate } from 'react-router-dom'
import { DEFAULT_HOTELIER_HOTEL, roleHref, useRole } from '../model/useRole'
import type { Role } from '../model/useRole'

const LABELS: Record<Role, string> = { guest: 'Гость', hotelier: 'Отельер', admin: 'Админ' }

export function RoleSwitch() {
  const { role, hotelId } = useRole()
  const navigate = useNavigate()
  return (
    <div className="flex items-center gap-1 text-sm" role="group" aria-label="Роль">
      {(Object.keys(LABELS) as Role[]).map((r) => (
        <button
          key={r}
          type="button"
          onClick={() => navigate(roleHref(r, r === 'hotelier' ? hotelId || DEFAULT_HOTELIER_HOTEL : undefined))}
          className={`rounded-md px-2 py-1 ${r === role ? 'bg-sky-700 text-white' : 'text-slate-700 hover:bg-slate-100'}`}
        >
          {LABELS[r]}
          {r === 'hotelier' && role === 'hotelier' ? ` ${hotelId}` : ''}
        </button>
      ))}
    </div>
  )
}
