import React from 'react'

const panel =
  'rounded-lg border border-[var(--tq-border)] bg-[var(--tq-panel)]'

export function Card({children, className = ''}: {children: React.ReactNode; className?: string}) {
  return <div className={`${panel} p-5 ${className}`}>{children}</div>
}

export function Button({
  children,
  variant = 'secondary',
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {variant?: 'primary' | 'secondary' | 'danger'}) {
  const styles: Record<string, string> = {
    primary: 'bg-[var(--tq-accent)] text-white border-transparent hover:opacity-90',
    secondary: 'border-[var(--tq-border)] hover:bg-black/5 dark:hover:bg-white/5',
    danger: 'border-[var(--tq-border)] text-[#e0432f] hover:bg-[#e0432f]/10',
  }
  return (
    <button
      {...rest}
      className={`rounded-md border px-3 py-1.5 disabled:opacity-40 disabled:cursor-not-allowed ${styles[variant]} ${rest.className ?? ''}`}
    >
      {children}
    </button>
  )
}

export function Field({
  label,
  error,
  children,
}: {
  label: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs uppercase tracking-wide text-[var(--tq-muted)]">{label}</span>
      {children}
      {error ? <span className="text-xs text-[#e0432f]">{error}</span> : null}
    </label>
  )
}

const control =
  'rounded-md border border-[var(--tq-border)] bg-transparent px-2.5 py-1.5 outline-none focus:border-[var(--tq-accent)]'

export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={`${control} ${props.className ?? ''}`} />
}

export function Select(props: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select {...props} className={`${control} bg-[var(--tq-panel)] ${props.className ?? ''}`}>
      {props.children}
    </select>
  )
}

export function Badge({children, tone = 'muted'}: {children: React.ReactNode; tone?: 'muted' | 'accent'}) {
  const c =
    tone === 'accent'
      ? 'border-[var(--tq-accent)] text-[var(--tq-accent)]'
      : 'border-[var(--tq-border)] text-[var(--tq-muted)]'
  return (
    <span className={`ml-1.5 rounded-full border px-1.5 py-0.5 text-[10px] uppercase ${c}`}>
      {children}
    </span>
  )
}

export function ColorSwatches({
  colors,
  value,
  onChange,
}: {
  colors: string[]
  value: string
  onChange: (c: string) => void
}) {
  return (
    <div className="flex gap-2">
      {colors.map((c) => (
        <button
          key={c}
          type="button"
          aria-label={`color ${c}`}
          onClick={() => onChange(c)}
          style={{background: c}}
          className={`h-6 w-6 rounded-full border-2 ${
            value === c ? 'border-[var(--tq-text)]' : 'border-transparent'
          }`}
        />
      ))}
    </div>
  )
}

export function ErrorList({errors}: {errors: string[]}) {
  if (errors.length === 0) return null
  return (
    <ul className="mb-3 rounded-md border border-[#e0432f] bg-[#e0432f]/10 px-4 py-2 text-sm text-[#e0432f]">
      {errors.map((e, i) => (
        <li key={i}>{e}</li>
      ))}
    </ul>
  )
}

export function Placeholder({title}: {title: string}) {
  return (
    <Card>
      <h2 className="text-lg font-semibold">{title}</h2>
      <p className="mt-2 text-[var(--tq-muted)]">This step is not wired up yet.</p>
    </Card>
  )
}

export function StepHeader({title, subtitle}: {title: string; subtitle?: string}) {
  return (
    <header className="mb-4">
      <h1 className="text-xl font-semibold">{title}</h1>
      {subtitle ? <p className="mt-1 text-[var(--tq-muted)]">{subtitle}</p> : null}
    </header>
  )
}
