import { useState } from 'react'
import type { ExplainedEvidence, ExplainedFact } from '@/shared/api'
import { Badge } from '@/shared/ui'
import { authorLabel, evidenceRoleLabel, mainValue, sourceLabel } from '@/shared/lib'

const n2 = (x: number) => x.toFixed(2).replace('.', ',')
const n3 = (x: number) => x.toFixed(3).replace('.', ',')

// Цепочка, которая даёт вклад: база × (0,5 + репутация) × асимметрия × свежесть → ограничение [0,05; 0,97] → logit → вклад.
function Chain({ e }: { e: ExplainedEvidence }) {
  const c = e.chain
  return (
    <div className="mt-1 font-mono text-[11px] leading-relaxed text-slate-700">
      <div>
        база {n2(c.base)}
        {e.author_type !== 'tool' ? <> × (0,5 + репутация {n2(c.trust ?? 0.5)} = {n2(c.trust_mult)})</> : <> × репутация —</>}
        {' '}× асимметрия {n2(c.asymmetry)} × свежесть {n2(c.freshness)} = {n3(c.product)}
      </div>
      <div>
        {c.clamped ? <>ограничено [0,05; 0,97] → <b>{n3(c.r)}</b></> : <>в границах [0,05; 0,97] → <b>{n3(c.r)}</b></>}
        {' '}→ logit {n2(e.logit)}
        {e.logit === 0 && ' (надёжность не выше 0,5: информации не несёт)'}
        {' '}→ вклад{' '}
        <b className={e.contribution > 0 ? 'text-emerald-700' : e.contribution < 0 ? 'text-rose-700' : 'text-amber-700'}>
          {e.contribution > 0 ? '+' : ''}
          {n2(e.contribution)}
        </b>
      </div>
    </div>
  )
}

// Раскрытие свидетельств факта «под капотом»: строка-сводка и по каждому свидетельству источник, автор, цепочка и вклад.
export function ExplainChain({ explain }: { explain: ExplainedFact }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="mt-2">
      <button type="button" className="text-xs text-sky-800 hover:underline" onClick={() => setOpen(!open)}>
        {open ? '▾' : '▸'} {explain.summary}
      </button>
      {open && (
        <div className="mt-1 space-y-2 rounded-md bg-slate-50 p-2">
          {explain.evidence.map((e) => (
            <div key={e.id} className={`text-xs ${e.role === 'dependent' ? 'opacity-60' : ''}`}>
              <div className="flex flex-wrap items-center gap-1">
                <Badge tone={e.role === 'for' ? 'green' : e.role === 'against' ? 'red' : 'gray'}>{evidenceRoleLabel(e.role)}</Badge>
                <b>{sourceLabel(e.source)}</b> ({authorLabel(e.author_type)}
                {e.author_type !== 'tool' ? ` ${e.author_id}` : ''}) утверждает «{mainValue(e.claimed, explain.value_field, explain.key)}»
                {e.group && <span className="text-slate-500">· группа независимости {e.group}</span>}
                {e.zero_weight && <Badge tone="amber">вес ≈ 0</Badge>}
              </div>
              {e.comment && <div className="italic text-slate-800">со слов отеля: «{e.comment}»</div>}
              <Chain e={e} />
            </div>
          ))}
          <div className="border-t border-slate-200 pt-1 text-xs text-slate-700">
            S = Σ вкладов = <b>{n2(explain.s)}</b> → σ(S) = <b>{n3(explain.sigma)}</b>, уверенность факта {n2(explain.confidence)}
            {explain.conflict && (
              <>
                {' '}· порог конфликта θ = {n2(explain.theta)}
                {explain.theta_lowered && ' (понижен: в споре участвует отельер)'}
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
