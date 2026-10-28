import type { ButtonHTMLAttributes, ReactNode } from 'react'

export { Icon } from './icon'
export type { IconName } from './icon'
export { Logo } from './Logo'

export type Tone = 'green' | 'blue' | 'amber' | 'red' | 'gray' | 'violet'

const TONES: Record<Tone, string> = {
  green: 'bg-ok/10 text-ok',
  blue: 'bg-brand-soft text-brand-link',
  amber: 'bg-warn-bg text-warn',
  red: 'bg-bad-bg text-bad',
  gray: 'bg-surface-muted text-ink-muted',
  violet: 'bg-brand-soft text-brand-link',
}

export function Badge({ tone = 'gray', title, children }: { tone?: Tone; title?: string; children: ReactNode }) {
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-caption font-medium ${TONES[tone]}`}
    >
      {children}
    </span>
  )
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'ghost'; small?: boolean }

export function Button({ variant = 'secondary', small, className = '', ...rest }: ButtonProps) {
  const base = 'inline-flex items-center justify-center gap-1 transition disabled:cursor-not-allowed disabled:opacity-50'
  const size = small ? 'h-8 rounded-sm px-2 text-caption font-medium' : 'h-10 rounded-inner px-3 text-button'
  const look = {
    primary: 'bg-brand text-white hover:opacity-90',
    secondary: 'bg-surface-muted text-ink hover:bg-line',
    ghost: 'text-brand-link hover:bg-brand-tint',
  }[variant]
  return <button type="button" className={`${base} ${size} ${look} ${className}`} {...rest} />
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <div className={`rounded-card bg-surface p-4 ${className}`}>{children}</div>
}

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="inline-flex items-center gap-2 text-body text-ink-muted">
      <span className="h-3 w-3 animate-spin rounded-full border-2 border-line border-t-brand" />
      {label}
    </span>
  )
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return <div className="rounded-inner bg-bad-bg px-3 py-2 text-body text-bad">{children}</div>
}

export function Notice({ children }: { children: ReactNode }) {
  return <div className="rounded-inner bg-warn-bg px-3 py-2 text-body text-warn">{children}</div>
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  return (
    <div className="fixed inset-0 z-[2000] flex items-start justify-center overflow-y-auto bg-ink/40 p-4" onClick={onClose}>
      <div
        role="dialog"
        aria-label={title}
        className="my-8 w-full max-w-2xl rounded-card bg-surface p-5"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-start gap-3">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button type="button" aria-label="Закрыть" className="ml-auto text-ink-muted hover:text-ink" onClick={onClose}>
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  )
}
