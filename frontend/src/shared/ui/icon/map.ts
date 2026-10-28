import {
  Building2,
  Check,
  CircleHelp,
  Columns3,
  Heart,
  MessageCircle,
  Moon,
  PawPrint,
  Search,
  TrainFront,
  TriangleAlert,
  Volume2,
  Waves,
  X,
  type LucideIcon,
} from 'lucide-react'

// Запасной вариант: имя иконки → lucide. Свой файл custom/<имя>.svg имеет приоритет.
export const FALLBACK = {
  heart: Heart,
  check: Check,
  question: CircleHelp,
  cross: X,
  conflict: TriangleAlert,
  hotelier: Building2,
  noise: Volume2,
  sea: Waves,
  metro: TrainFront,
  night: Moon,
  pet: PawPrint,
  compare: Columns3,
  chat: MessageCircle,
  search: Search,
} satisfies Record<string, LucideIcon>

export type IconName = keyof typeof FALLBACK
