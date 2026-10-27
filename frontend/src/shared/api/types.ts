// Типы ответов JSON API (backend/internal/adapter/http). Фронтенд не интерпретирует факты сам:
// статусы, тексты и объяснения приходят готовыми.

export type Importance = 'must' | 'prefer'
export type WishStatus = 'match' | 'chance' | 'uncertain' | 'near' | 'mismatch' | 'unknown'

export interface Desire {
  key: string
  op?: string
  value?: unknown
  importance?: Importance
  min_floor?: number
}

export interface Query {
  wishes: Desire[]
  filters: { price_max?: number; stars_min?: number }
  session_id?: string
  limit?: number
}

export interface Card {
  mock: boolean
  stars: number
  price_eur: number
  breakfast: boolean
  wifi: boolean
}

export interface Source {
  id: number
  source: string
  author_type: string
  reliability: number
  contribution: number
  observed_at: string
}

export interface BookingWish {
  facade_id: number
  side: string
  min_floor: number
  rooms: number
  share: number
  probability: number
  hotel_confirmed: boolean
  text: string
}

export interface WishEval {
  key: string
  title: string
  importance: Importance
  status: WishStatus
  value?: unknown
  confidence: number
  p_base: number
  p_wish: number
  probability: number
  score: number
  guarantee: boolean
  conflict?: string
  caveat?: string
  booking_wish?: BookingWish
  fact_id?: number
  observed_at?: string
  valid_until?: string
  sources: Source[]
}

export interface Variant {
  category_id: number
  name: string
  rooms_count: number
  price_eur: number
  score: number
  guaranteed: boolean
  violations: string[]
  unknown: string[]
  wishes: WishEval[]
}

export interface OtherConflict {
  category: string
  key: string
  description: string
}

export interface SearchHotel {
  hotel_id: number
  name: string
  district: string
  card: Card
  score: number
  violations: string[]
  best: Variant
  alternative?: Variant
  other_conflicts: OtherConflict[]
}

export interface Explanation {
  hotel_id: number
  headline: string
  lines: string[]
  notes: string[]
}

export type GroupKind = 'full' | 'open' | 'compromise' | 'ranked'

export interface Group {
  kind: GroupKind
  title?: string
  failed_key?: string
  must_violated?: boolean
  hotels: SearchHotel[]
  explanations: Explanation[]
}

export interface Difference {
  key: string
  title: string
  distance: number
  weight: number
  unknown?: boolean
}

export interface SimilarHotel {
  id: number
  name: string
  district: string
  card: Card
  distance: number
  differences: Difference[]
}

export interface ResultsEvent {
  kind: 'search' | 'similar'
  query: Query
  considered?: number
  dropped?: number
  hotels?: SearchHotel[]
  groups_lead?: string
  groups?: Group[]
  explanations?: Explanation[]
  base?: { id: number; name: string }
  similar?: SimilarHotel[]
}

export interface StepEvent {
  n: number
  mode: string
  decision: string
  tool?: string
  input?: Record<string, unknown>
  output?: Record<string, unknown>
}

export interface Clarification {
  kind: 'ambiguity' | 'importance'
  phrase: string
  question: string
  options: { id: string; label: string }[]
}

export interface MessageEvent {
  text: string
  source: 'template' | 'llm'
}

export interface DoneEvent {
  session_id: string
  mode: string
  clarifications_asked: number
}

export interface DialogExample {
  id: string
  title: string
  text: string
  next: string[]
}

export interface Meta {
  llm_mode: 'llm' | 'cache'
  llm_model?: string
}

// Карточка отеля.

export interface Evidence {
  id: number
  source: string
  author_type: string
  claimed: Record<string, unknown>
  reliability: number
  contribution: number
  observed_at: string
}

export interface Fact {
  id: number
  key: string
  title: string
  entity_type: string
  value: Record<string, unknown>
  confidence: number
  source: string
  method: string
  observed_at: string
  valid_until?: string
  stale?: boolean
  weak_disagreement?: boolean
  conflict?: string
  evidence: Evidence[]
}

export interface FacadeValue {
  key: string
  value: Record<string, unknown>
  confidence: number
}

export interface Facade {
  id: number
  azimuth_deg: number
  side: string
  faces_street: boolean
  courtyard: boolean
  mid: [number, number]
  values: FacadeValue[]
}

export interface Placement {
  facade_id: number
  floor_from: number
  floor_to: number
  rooms_count: number
  source: string
  confidence: number
}

export interface CategoryFact {
  value: Record<string, unknown>
  confidence: number
  source: string
  stale?: boolean
  weak_disagreement?: boolean
  conflict?: string
  evidence: Evidence[]
}

export interface CategoryChar {
  key: string
  title: string
  target: string
  probability: number
  guarantee: boolean
  shares: Record<string, number>
  unknown: number
  wish?: { facade_id: number; side: string; min_floor: number; rooms: number; share: number; text: string }
  fact?: CategoryFact
}

export interface Category {
  id: number
  name: string
  rooms_count: number
  placements: Placement[]
  characteristics: CategoryChar[]
}

export interface Rooms {
  hotel_id: number
  facades: Facade[]
  categories: Category[]
}

export interface HotelCardData {
  id: number
  name: string
  district: string
  lat: number
  lon: number
  card: Card
  facts: Fact[]
  rooms: Rooms | null
}

export interface Characteristic {
  key: string
  title: string
  description: string
  level: string
  value_type: string
  value_field: string
  favorable_values?: string[]
}

// Шортлист, сравнение, контракт, бронь.

export interface Shown {
  id: number
  name: string
}

export interface CompareSide {
  hotel_id: number
  label: string
  category_id: number
  category: string
  price_eur: number
  score: number
  guaranteed: boolean
  violations: string[]
  conflicts: string[]
}

export interface CompareDiff {
  key: string
  title: string
  day_part: string
  importance: string
  better: number
  texts: string[]
  short: string[]
}

export interface CompareUncertain {
  side: number
  hotel_id: number
  key: string
  title: string
  reason: string
  action: string
}

export interface Compare {
  categories: boolean
  hotel_id?: number
  wishes: string[]
  winner: number
  verdict: string
  sides: CompareSide[]
  violations: string[][]
  differences: CompareDiff[]
  same: CompareDiff[]
  tradeoff: string
  uncertain: CompareUncertain[]
  days: { part: string; title: string; keys: string[] }[]
}

export type ContractKind =
  | 'guaranteed'
  | 'chance'
  | 'wish_pending'
  | 'wish_confirmed'
  | 'wish_refused'
  | 'uncertain'
  | 'unknown'
  | 'mismatch'

export interface ContractLine {
  key: string
  title: string
  importance: Importance
  kind: ContractKind
  probability: number
  confidence: number
  text: string
  source?: string
  conflict?: string
  action?: 'ask_hotel'
}

export interface ContractOffer {
  facade_id: number
  side: string
  floor_from: number
  probability: number
  text: string
  hotel_confirmed: boolean
  hotel_refused: boolean
}

export interface Contract {
  hotel_id: number
  hotel_name: string
  category_id: number
  category: string
  price_eur: number
  with_wish: boolean
  offer?: ContractOffer
  lines: ContractLine[]
}

export interface AskResult {
  message: string
  question_id?: number
  status: 'created' | 'existing' | 'queued' | 'none'
  sent: boolean
}

export interface PlacedWish {
  text: string
  facade_id: number
  status: 'waiting' | 'confirmed' | 'refused'
  question_id?: number
  question_reused?: boolean
}

export interface Booking {
  id: number
  hotel_id: number
  hotel_name?: string
  category_id: number
  category?: string
  price_eur?: number
  wish?: PlacedWish
  contract: ContractLine[]
}
