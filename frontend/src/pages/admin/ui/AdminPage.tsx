import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AgentJournal } from '@/widgets/agent-journal'
import { ConflictsBoard } from '@/widgets/conflicts-board'
import { FactsExplorer } from '@/widgets/facts-explorer'
import { RegistryBoard } from '@/widgets/registry-board'
import { SummaryBoard } from '@/widgets/summary-board'

type Tab = 'journal' | 'facts' | 'conflicts' | 'registry' | 'summary'

const TABS: { id: Tab; title: string }[] = [
  { id: 'journal', title: 'Журнал агентов' },
  { id: 'facts', title: 'Факты и свидетельства' },
  { id: 'conflicts', title: 'Конфликты' },
  { id: 'registry', title: 'Реестр' },
  { id: 'summary', title: 'Сводка' },
]

// «Под капотом» (роль админа): журнал агентов, факты со свидетельствами, конфликты.
export function AdminPage() {
  const [tab, setTab] = useState<Tab>('journal')
  return (
    <div className="mx-auto max-w-5xl space-y-4 px-4 py-4">
      <div>
        <Link to="/" className="text-xs text-sky-800 hover:underline">
          ← к поиску
        </Link>
        <h1 className="text-2xl font-bold">Под капотом</h1>
        <p className="text-sm text-slate-600">Что делали агенты, из каких свидетельств сложен каждый факт и где источники спорят.</p>
      </div>
      <div className="flex flex-wrap gap-1 border-b border-slate-200 pb-1" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={`rounded-md px-3 py-1.5 text-sm ${tab === t.id ? 'bg-sky-700 text-white' : 'text-slate-700 hover:bg-slate-100'}`}
          >
            {t.title}
          </button>
        ))}
      </div>
      {tab === 'journal' && <AgentJournal />}
      {tab === 'facts' && <FactsExplorer />}
      {tab === 'conflicts' && <ConflictsBoard />}
      {tab === 'registry' && <RegistryBoard />}
      {tab === 'summary' && <SummaryBoard />}
    </div>
  )
}
