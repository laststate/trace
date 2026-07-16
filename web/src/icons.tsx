import type { LucideIcon } from 'lucide-react'
import {
  Activity,
  AlertTriangle,
  Archive,
  ArrowLeftRight,
  Box,
  ChevronRight,
  CircuitBoard,
  ClipboardList,
  Gauge,
  HardDrive,
  LayoutDashboard,
  LogIn,
  LogOut,
  Power,
  Radio,
  RefreshCw,
  Search,
  Settings,
  Shield,
  Skull,
  Tag,
  Upload,
  Zap,
} from 'lucide-react'
import type { View } from './nav'

const NAV_ICONS: Record<View, LucideIcon> = {
  overview: LayoutDashboard,
  issues: AlertTriangle,
  events: Zap,
  devices: HardDrive,
  releases: Upload,
  artifacts: Archive,
  hardware: CircuitBoard,
  boots: Power,
  alerts: Radio,
  channels: Tag,
  relays: ArrowLeftRight,
  projects: Box,
  dead: Skull,
  audit: ClipboardList,
  settings: Settings,
}

export function NavIcon({ view, size = 16 }: { view: View; size?: number }) {
  const Icon = NAV_ICONS[view] || Activity
  return <Icon size={size} strokeWidth={1.75} className="nav-icon" aria-hidden />
}

export {
  Activity,
  AlertTriangle,
  Archive,
  ChevronRight,
  Gauge,
  LogIn,
  LogOut,
  RefreshCw,
  Search,
  Settings,
  Shield,
}
