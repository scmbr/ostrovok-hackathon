// Районы приходят из API кодами (`districts.code`); для интерфейса нужны русские названия.
// Незнакомый код показываем как есть: новый район не должен ломать экран.
const DISTRICTS: Record<string, string> = {
  barceloneta: 'Барселонета',
  gothic: 'Готический квартал',
  eixample: 'Эшампле',
  vila_olimpica: 'Вила-Олимпика',
  diagonal_mar: 'Диагональ-Мар',
  poblenou: 'Побленоу',
}

export const districtLabel = (code: string): string => DISTRICTS[code] ?? code
