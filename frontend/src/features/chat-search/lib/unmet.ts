// Желания, которые система не умеет проверять, API отдаёт только строкой сообщения агента:
// «Пока не умею учитывать: «а», «б» — записала в журнал пожеланий.» (строка собрана кодом, не моделью).
const UNMET_LINE = /Пока не умею учитывать:\s*(.+?)\s*—\s*записала/

export function unmetFromMessage(text: string | undefined): string[] {
  const m = text?.match(UNMET_LINE)
  if (!m) return []
  return [...m[1].matchAll(/«([^»]+)»/g)].map((x) => x[1])
}
