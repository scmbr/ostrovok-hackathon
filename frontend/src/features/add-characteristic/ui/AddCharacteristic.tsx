import { useEffect, useRef, useState } from 'react'
import type { CharacteristicDraft, Job } from '@/shared/api'
import { ApiError } from '@/shared/api'
import { Badge, Button, Card, ErrorNote, Notice } from '@/shared/ui'
import { getCharacteristics, resetCharacteristics } from '@/entities/fact'
import { computeCharacteristic, draftCharacteristic, getJob, getLlmMode, saveCharacteristic } from '../api/addApi'

// Первые формулировки встроенных заготовок кеша (backend/internal/adapter/llm/cache/*.yaml) и ключи их характеристик:
// без LLM ответ есть только на них.
const EXAMPLES = [
  { text: 'есть ли рядом круглосуточный магазин', key: 'night_shop' },
  { text: 'есть ли рядом круглосуточная аптека', key: 'night_pharmacy' },
  { text: 'можно ли с собакой', key: 'pets_allowed' },
  { text: 'затемняющие шторы', key: 'blackout_curtains' },
]

interface Props {
  // Реестр на сервере изменился: список нужно перечитать.
  onSaved: () => void
  // Предзаполненное описание (например, из журнала неизвестных пожеланий); ничего не создаёт само.
  initialText?: string
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
        <span className="text-xs text-ink-muted">{job.message}</span>
      </div>
      <div className="h-2 rounded bg-surface-muted">
        <div className="h-2 rounded bg-brand transition-all" style={{ width: `${pct}%` }} />
      </div>
      {job.errors.length > 0 && (
        <ul className="list-disc pl-5 text-xs text-bad">
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
export function AddCharacteristic({ onSaved, initialText = '' }: Props) {
  const [text, setText] = useState(initialText)
  const [draft, setDraft] = useState<CharacteristicDraft | null>(null)
  const [yaml, setYaml] = useState('')
  const [errors, setErrors] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [savedKey, setSavedKey] = useState<string | null>(null)
  const [job, setJob] = useState<Job | null>(null)
  const [cacheMode, setCacheMode] = useState(false)
  // Ключи характеристик, которые уже есть в реестре: их заготовки не предлагаем.
  const [existing, setExisting] = useState<Set<string>>(new Set())
  const timer = useRef<number | null>(null)

  useEffect(() => {
    getLlmMode().then((m) => setCacheMode(m === 'cache'), () => {})
    getCharacteristics().then((cs) => setExisting(new Set(cs.map((c) => c.key))), () => {})
  }, [])

  const available = EXAMPLES.filter((x) => !existing.has(x.key))

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
      setExisting((prev) => new Set(prev).add(r.key))
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
      <p className="text-xs text-ink-muted">
        Модель только описывает, как получить характеристику из существующих инструментов; значения фактов она не пишет. Перезапуск сервера не нужен.
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <input
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="например: есть ли рядом круглосуточный магазин"
          className="min-w-0 flex-1 rounded-inner border border-line px-2 py-1 text-sm"
          onKeyDown={(e) => e.key === 'Enter' && text.trim() && !busy && makeDraft()}
        />
        <Button variant="primary" disabled={busy || !text.trim()} onClick={makeDraft}>
          Предпросмотр
        </Button>
      </div>
      {cacheMode ? (
        available.length > 0 ? (
          <>
            <div className="mt-1 flex flex-wrap items-center gap-2 text-xs">
              <span className="text-ink">В режиме без LLM доступны:</span>
              {available.map((x) => (
                <button key={x.key} type="button" className="text-brand-link hover:underline" onClick={() => setText(x.text)}>
                  {x.text}
                </button>
              ))}
            </div>
            <p className="mt-1 text-xs text-ink-muted">Без ключа LLM черновик есть только для этих формулировок; выбор подставляет точный текст заготовки.</p>
          </>
        ) : (
          <p className="mt-1 text-xs text-ink-muted">Заготовки без LLM исчерпаны, для своих описаний нужен ключ.</p>
        )
      ) : null}

      {error && (
        <div className="mt-2">
          <ErrorNote>{error}</ErrorNote>
        </div>
      )}
      {errors.length > 0 && (
        <ul className="mt-2 list-disc pl-5 text-sm text-bad">
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
          <p className="text-xs text-ink">
            Синонимы (по ним гость найдёт характеристику текстом без модели) предложены кодом из названия и вашей фразы; правьте их в YAML (поле
            <code> synonyms</code>). Фразы не должны пересекаться с синонимами других характеристик.
          </p>
          <textarea
            value={yaml}
            onChange={(e) => setYaml(e.target.value)}
            spellCheck={false}
            rows={Math.min(24, yaml.split('\n').length + 1)}
            className="w-full rounded-inner border border-line p-2 font-mono text-xs"
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
            <p className="text-sm text-ink">Готово. Попробуйте в роли гостя фразу, подходящую к характеристике, например «ночной магазин рядом».</p>
          )}
        </div>
      )}
    </Card>
  )
}
