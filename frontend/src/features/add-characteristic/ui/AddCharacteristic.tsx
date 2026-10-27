import { useEffect, useRef, useState } from 'react'
import type { CharacteristicDraft, Job } from '@/shared/api'
import { ApiError } from '@/shared/api'
import { Badge, Button, Card, ErrorNote, Notice } from '@/shared/ui'
import { resetCharacteristics } from '@/entities/fact'
import { computeCharacteristic, draftCharacteristic, getJob, saveCharacteristic } from '../api/addApi'

const EXAMPLES = ['есть ли рядом круглосуточный магазин', 'есть ли рядом круглосуточная аптека', 'можно ли с собакой']

interface Props {
  // Реестр на сервере изменился: список нужно перечитать.
  onSaved: () => void
}

function JobBar({ job }: { job: Job }) {
  const pct = job.total > 0 ? Math.round((job.done / job.total) * 100) : 0
  return (
    <div className="space-y-1 text-sm">
      <div className="flex items-center gap-2">
        <Badge tone={job.status === 'done' ? 'green' : job.status === 'failed' ? 'red' : 'blue'}>
          {{ running: 'считаю', done: 'готово', failed: 'ошибка' }[job.status]}
        </Badge>
        <span>
          {job.done} из {job.total}
        </span>
        <span className="text-xs text-slate-500">{job.message}</span>
      </div>
      <div className="h-2 rounded bg-slate-100">
        <div className="h-2 rounded bg-sky-600 transition-all" style={{ width: `${pct}%` }} />
      </div>
      {job.errors.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-rose-800">
          {job.errors.map((e, i) => (
            <li key={i}>{e}</li>
          ))}
        </ul>
      )}
    </div>
  )
}

// Добавление характеристики текстом без пересборки: текст → предпросмотр YAML с проверкой → сохранить → посчитать для
// демо-отелей. Синонимы для разбора запроса гостя без модели предлагает код, их можно править.
export function AddCharacteristic({ onSaved }: Props) {
  const [text, setText] = useState('')
  const [draft, setDraft] = useState<CharacteristicDraft | null>(null)
  const [yaml, setYaml] = useState('')
  const [errors, setErrors] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [savedKey, setSavedKey] = useState<string | null>(null)
  const [job, setJob] = useState<Job | null>(null)
  const timer = useRef<number | null>(null)

  useEffect(() => () => void (timer.current && window.clearTimeout(timer.current)), [])

  const fail = (e: unknown) => {
    if (e instanceof ApiError) {
      setError(e.message)
      setErrors(e.details)
    } else {
      setError((e as Error).message)
      setErrors([])
    }
  }

  const makeDraft = async () => {
    setBusy(true)
    setError(null)
    setErrors([])
    setSavedKey(null)
    setJob(null)
    try {
      const d = await draftCharacteristic(text)
      setDraft(d)
      setYaml(d.yaml)
      setErrors(d.errors)
    } catch (e) {
      setDraft(null)
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    setError(null)
    setErrors([])
    try {
      const r = await saveCharacteristic(yaml)
      resetCharacteristics()
      setSavedKey(r.key)
      onSaved()
    } catch (e) {
      fail(e)
    } finally {
      setBusy(false)
    }
  }

  const poll = (id: string) => {
    getJob(id).then(
      (j) => {
        setJob(j)
        if (j.status === 'running') timer.current = window.setTimeout(() => poll(id), 1000)
      },
      (e: Error) => setError(e.message),
    )
  }

  const compute = async () => {
    if (!savedKey) return
    setError(null)
    try {
      const r = await computeCharacteristic(savedKey)
      poll(r.job_id)
    } catch (e) {
      fail(e)
    }
  }

  return (
    <Card>
      <h3 className="font-semibold">Добавить характеристику текстом</h3>
      <p className="text-xs text-slate-500">
        Модель только описывает, как получить характеристику из существующих инструментов; значения фактов она не пишет. Перезапуск сервера не нужен.
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="например: есть ли рядом круглосуточный магазин"
          className="min-w-0 flex-1 rounded-md border border-slate-300 px-2 py-1 text-sm"
          onKeyDown={(e) => e.key === 'Enter' && text.trim() && !busy && makeDraft()}
        />
        <Button variant="primary" disabled={busy || !text.trim()} onClick={makeDraft}>
          Предпросмотр
        </Button>
      </div>
      <div className="mt-1 flex flex-wrap gap-2 text-xs">
        {EXAMPLES.map((x) => (
          <button key={x} type="button" className="text-sky-800 hover:underline" onClick={() => setText(x)}>
            {x}
          </button>
        ))}
      </div>

      {error && (
        <div className="mt-2">
          <ErrorNote>{error}</ErrorNote>
        </div>
      )}
      {errors.length > 0 && (
        <ul className="mt-2 list-disc pl-5 text-sm text-rose-800">
          {errors.map((e, i) => (
            <li key={i}>{e}</li>
          ))}
        </ul>
      )}

      {draft && !savedKey && (
        <div className="mt-3 space-y-2">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <b>{draft.title}</b>
            <Badge tone="gray">{draft.key}</Badge>
            <Badge tone={draft.mode === 'llm' ? 'green' : 'blue'}>{draft.mode === 'llm' ? 'модель' : 'заготовка из кеша'}</Badge>
          </div>
          {draft.notes.map((n, i) => (
            <Notice key={i}>{n}</Notice>
          ))}
          <p className="text-xs text-slate-600">
            Синонимы (по ним гость найдёт характеристику текстом без модели) предложены кодом из названия и вашей фразы; правьте их в YAML (поле
            <code> synonyms</code>). Фразы не должны пересекаться с синонимами других характеристик.
          </p>
          <textarea
            value={yaml}
            onChange={(e) => setYaml(e.target.value)}
            spellCheck={false}
            rows={Math.min(24, yaml.split('\n').length + 1)}
            className="w-full rounded-md border border-slate-300 p-2 font-mono text-xs"
          />
          <Button variant="primary" disabled={busy || !yaml.trim()} onClick={save}>
            Сохранить
          </Button>
        </div>
      )}

      {savedKey && (
        <div className="mt-3 space-y-2">
          <Notice>
            Характеристика <b>{savedKey}</b> сохранена и уже доступна в работающем сервере. Значений по отелям пока нет.
          </Notice>
          {!job && (
            <Button variant="primary" onClick={compute}>
              Посчитать для демо-отелей
            </Button>
          )}
          {job && <JobBar job={job} />}
          {job?.status === 'done' && (
            <p className="text-sm text-slate-700">Готово. Попробуйте в роли гостя фразу, подходящую к характеристике, например «ночной магазин рядом».</p>
          )}
        </div>
      )}
    </Card>
  )
}
