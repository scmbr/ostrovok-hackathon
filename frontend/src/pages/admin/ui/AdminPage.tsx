import { useState } from 'react'
import { Link } from 'react-router-dom'
import { AgentJournal } from '@/widgets/agent-journal'
import { ConflictsBoard } from '@/widgets/conflicts-board'
import { FactsExplorer } from '@/widgets/facts-explorer'
import { RegistryBoard } from '@/widgets/registry-board'
import { SummaryBoard } from '@/widgets/summary-board'
import { UnmetWishesBoard } from '@/widgets/unmet-wishes-board'

type Tab = 'journal' | 'facts' | 'conflicts' | 'unmet' | 'registry' | 'summary'

const TABS: { id: Tab; title: string }[] = [
  { id: 'journal', title: 'Журнал агентов' },
  { id: 'facts', title: 'Факты и свидетельства' },
  { id: 'conflicts', title: 'Конфликты' },
  { id: 'unmet', title: 'Неизвестные пожелания' },
  { id: 'registry', title: 'Реестр' },
  { id: 'summary', title: 'Сводка' },
]

// «Под капотом» (роль админа): журнал агентов, факты со свидетельствами, конфликты.
export function AdminPage() {
  const [tab, setTab] = useState<Tab>('journal')
  // Описание из журнала пожеланий для формы добавления характеристики на вкладке «Реестр».
  const [proposed, setProposed] = useState<string | undefined>()
  const propose = (description: string) => {
    setProposed(description)
    setTab('registry')
  }
  return (
    <div className="mx-auto max-w-5xl space-y-4 px-4 py-4">
      <div>
        <Link to="/search" className="text-xs text-brand-link hover:underline">
          ← к поиску
        </Link>
        <h1 className="text-2xl font-bold">Под капотом</h1>
        <p className="text-sm text-ink">Что делали агенты, из каких свидетельств сложен каждый факт и где источники спорят.</p>
      </div>
      <div className="flex flex-wrap gap-1 border-b border-line pb-1" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={tab === t.id}
            onClick={() => setTab(t.id)}
            className={`rounded-inner px-3 py-1.5 text-sm ${tab === t.id ? 'bg-brand text-white' : 'text-ink hover:bg-surface-muted'}`}
          >
            {t.title}
          </button>
        ))}
      </div>
      {tab === 'journal' && <AgentJournal />}
      {tab === 'facts' && <FactsExplorer />}
      {tab === 'conflicts' && <ConflictsBoard />}
      {tab === 'unmet' && <UnmetWishesBoard onPropose={propose} />}
      {tab === 'registry' && <RegistryBoard key={proposed} initialText={proposed} />}
      {tab === 'summary' && <SummaryBoard />}
    </div>
  )
}
