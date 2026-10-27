import type { ButtonHTMLAttributes, ReactNode } from 'react'

export type Tone = 'green' | 'blue' | 'amber' | 'red' | 'gray' | 'violet'

const TONES: Record<Tone, string> = {
  green: 'bg-emerald-100 text-emerald-800 ring-emerald-200',
  blue: 'bg-sky-100 text-sky-800 ring-sky-200',
  amber: 'bg-amber-100 text-amber-900 ring-amber-200',
  red: 'bg-rose-100 text-rose-800 ring-rose-200',
  gray: 'bg-slate-100 text-slate-700 ring-slate-200',
  violet: 'bg-violet-100 text-violet-800 ring-violet-200',
}

export function Badge({ tone = 'gray', title, children }: { tone?: Tone; title?: string; children: ReactNode }) {
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ring-1 ring-inset ${TONES[tone]}`}
    >
      {children}
    </span>
  )
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'ghost'; small?: boolean }

export function Button({ variant = 'secondary', small, className = '', ...rest }: ButtonProps) {
  const base = 'inline-flex items-center justify-center gap-1 rounded-md font-medium transition disabled:cursor-not-allowed disabled:opacity-50'
  const size = small ? 'px-2 py-1 text-xs' : 'px-3 py-1.5 text-sm'
  const look = {
    primary: 'bg-sky-700 text-white hover:bg-sky-800',
    secondary: 'bg-white text-slate-800 ring-1 ring-inset ring-slate-300 hover:bg-slate-50',
    ghost: 'text-sky-800 hover:bg-sky-50',
  }[variant]
  return <button type="button" className={`${base} ${size} ${look} ${className}`} {...rest} />
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-lg border border-slate-200 bg-white p-4 shadow-sm ${className}`}>{children}</div>
}

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-2 text-sm text-slate-500">
      <span className="h-3 w-3 animate-spin rounded-full border-2 border-slate-300 border-t-sky-600" />
      {label}
    </span>
  )
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return <div className="rounded-md bg-rose-50 px-3 py-2 text-sm text-rose-800 ring-1 ring-inset ring-rose-200">{children}</div>
}

export function Notice({ children }: { children: ReactNode }) {
  return <div className="rounded-md bg-amber-50 px-3 py-2 text-sm text-amber-900 ring-1 ring-inset ring-amber-200">{children}</div>
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-[2000] flex items-start justify-center overflow-y-auto bg-slate-900/40 p-4" onClick={onClose}>
      <div
        role="dialog"
        aria-label={title}
        className="my-8 w-full max-w-2xl rounded-lg bg-white p-5 shadow-xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-start gap-3">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button type="button" aria-label="Закрыть" className="ml-auto text-slate-500 hover:text-slate-900" onClick={onClose}>
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}
