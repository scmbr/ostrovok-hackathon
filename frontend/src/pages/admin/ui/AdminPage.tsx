import { Link } from 'react-router-dom'
import { Card } from '@/shared/ui'

// Заглушка: экран «под капотом» делает задача 7.2.
export function AdminPage() {
  return (
    <div className="mx-auto max-w-3xl space-y-3 px-4 py-6">
      <Card>
        <h1 className="text-xl font-bold">Под капотом</h1>
        <p className="mt-1 text-sm text-slate-600">Факты, источники, противоречия и журнал агентов появятся в следующей задаче этапа 7.</p>
        <Link to="/" className="mt-2 inline-block text-sm text-sky-800 hover:underline">
          ← к поиску
        </Link>
      </Card>
    </div>
  )
}
