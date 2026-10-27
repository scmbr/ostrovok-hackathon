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
  // Репутация автора-человека; у расчёта нет.
  author_trust?: number
  // Пояснение отельера к ответу («со слов отеля»); на значение не влияет.
  comment?: string
}

export interface Fact {
  id: number
  key: string
  title: string
  entity_type: string
  entity_id: string
  value: Record<string, unknown>
  confidence: number
  source: string
  method: string
  observed_at: string
  valid_until?: string
  stale?: boolean
  weak_disagreement?: boolean
  conflict?: string
  // «Со слов отеля; часть прошлых утверждений расчёт не подтвердил» — тот же текст, что в выдаче (domain.TrustNote).
  trust_note?: string
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
  trust_note?: string
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
  values?: string[]
  favorable_values?: string[]
  // Для характеристик, добавленных текстом.
  origin?: string
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
  // Цена из мок-карточки отеля.
  price_mock?: boolean
  wish?: PlacedWish
  contract: ContractLine[]
}

// Кабинет отельера.

export type ReasonKind = 'no_data' | 'low_confidence' | 'conflict' | 'guests'
export type QuestionFormat = 'confirm' | 'open'

export interface InboxQuestion {
  id: number
  characteristic: string
  title: string
  format: QuestionFormat
  entity_type?: string
  entity_id?: string
  question: string
  reason: string
  reason_kind: ReasonKind
  value: number
  score: { demand: number; demand_norm: number; uncertainty: number; impact: number; categories_changed: number; categories: number }
  status: string
  sent_at?: string
}

export interface WishRequest {
  question_id: number
  booking_id: number
  category: string
  wishes: Record<string, unknown>
  created_at: string
}

export interface Inbox {
  hotel_id: number
  hotel: string
  budget: { limit: number; window_days: number; sent: number }
  questions: InboxQuestion[]
  waiting: number
  low_value: number
  wish_requests: WishRequest[]
}

export interface EvidenceSnap {
  id: number
  source: string
  author_type: string
  author_trust: number
  claimed: Record<string, unknown>
  reliability: number
  contribution: number
  observed_at: string
  comment?: string
}

export interface FactSnap {
  found: boolean
  value?: Record<string, unknown>
  confidence: number
  conflict?: string
  weak_disagreement?: boolean
  stale?: boolean
  evidence: EvidenceSnap[]
}

export type ConflictChange = 'none' | 'opened' | 'closed' | 'remains'

// Факт до и после ответа: после записи свидетельства и пересчёта репутации.
export interface FactChange {
  entity_type: string
  entity_id: string
  key: string
  title: string
  value_field: string
  before: FactSnap
  after: FactSnap
  conflict_change: ConflictChange
  // Почему закрылся конфликт: agreed — стороны сошлись, weight_dropped — вес утверждения отеля стал нулевым.
  resolution?: { kind: 'agreed' | 'weight_dropped' | 'below_threshold' | 'evidence_removed'; text: string }
}

export interface TrustChange {
  before: number
  after: number
  agree: number
  total: number
}

export interface AnswerOutcome {
  question_id: number
  status: string
  note?: string
  parsed_by?: string
  claimed?: Record<string, unknown>
  follow_up?: string
  trust?: TrustChange
  change?: FactChange
}

export interface PersonaPreview {
  question_id: number
  persona: string
  answer: string
  simulated: boolean
  found: boolean
  claimed?: Record<string, unknown>
  follow_up?: string
  note?: string
  parsed_by: string
}

export interface FactEditOutcome {
  fact_id: number
  characteristic: string
  action: string
  claimed?: Record<string, unknown>
  evidence_written: boolean
  recheck: boolean
  note?: string
  trust?: TrustChange
  change?: FactChange
}

export interface HotelierHotel {
  id: number
  name: string
  district: string
  persona?: string
}

export interface MissedDemand {
  key: string
  title: string
  demand: number
  text: string
  question_id?: number
}

// «Под капотом» (роль админа).

export interface AgentRun {
  agent: string
  session_id: string
  hotel_id?: number
  mode: string
  steps: number
  refusals: number
  started_at: string
  ended_at: string
  title: string
}

export interface AgentRunStep {
  step: number
  mode: string
  decision: string
  tool?: string
  input: Record<string, unknown>
  output: Record<string, unknown>
  refusal: boolean
  created_at: string
}

export interface ReliabilityChain {
  base: number
  trust?: number
  trust_mult: number
  asymmetry: number
  freshness: number
  product: number
  r: number
  clamped: boolean
}

export interface ExplainedEvidence {
  id: number
  source: string
  author_type: string
  author_id: string
  claimed: Record<string, unknown>
  comment?: string
  observed_at: string
  chain: ReliabilityChain
  logit: number
  role: 'for' | 'against' | 'dependent'
  contribution: number
  group?: string
  zero_weight: boolean
}

export interface ExplainedFact {
  entity_type: string
  entity_id: string
  key: string
  title: string
  value_field: string
  value: Record<string, unknown>
  confidence: number
  s: number
  sigma: number
  summary: string
  stale?: boolean
  weak_disagreement?: boolean
  conflict?: string
  theta: number
  theta_lowered?: boolean
  evidence: ExplainedEvidence[]
}

export interface ConflictSide {
  value: string
  weight: number
  evidence: { author_type: string; author_id: string; source: string; contribution: number; comment?: string; zero_weight?: boolean }[]
}

export interface AdminConflict {
  id: number
  entity_type: string
  entity_id: string
  key: string
  title: string
  hotel_id?: number
  status: string
  description: string
  resolution?: string
  resolution_kind?: string
  detected_at: string
  sides: ConflictSide[]
  theta?: number
  theta_lowered?: boolean
  theta_reason?: string
  question?: { id: number; status: string; format: string }
}

export interface RecomputeTask {
  id: number
  hotel_id: number
  key: string
  reason: string
  status: string
  outcome?: string
  created_at: string
  done_at?: string
}

export interface StaleFactRow {
  entity_type: string
  entity_id: string
  key: string
  hotel_id?: number
  valid_until?: string
}
