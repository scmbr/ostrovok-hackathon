import type { Category } from '@/shared/api'

// Категория для кнопки «Забронировать» в шапке: с желаниями — лучшая по ним (гарантия весит как 100 %, при равенстве дешевле),
// без желаний — самая дешёвая. preferredId — вариант, который уже выбрал поиск для этого гостя.
export function pickCategory(categories: Category[], wishKeys: string[], preferredId?: number): Category | undefined {
  if (!categories.length) return undefined
  const preferred = categories.find((c) => c.id === preferredId)
  if (preferred) return preferred
  const score = (c: Category) =>
    wishKeys.reduce((sum, k) => {
      const ch = c.characteristics.find((x) => x.key === k)
      return sum + (ch ? (ch.guarantee ? 1 : ch.probability) : 0)
    }, 0)
  const price = (c: Category) => c.price_eur ?? Number.MAX_SAFE_INTEGER
  return [...categories].sort((a, b) => score(b) - score(a) || price(a) - price(b))[0]
}
