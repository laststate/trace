export type View =
  | 'overview' | 'issues' | 'events' | 'devices' | 'releases'
  | 'artifacts' | 'alerts' | 'channels' | 'relays' | 'projects' | 'hardware'
  | 'boots' | 'dead' | 'audit' | 'settings' | 'analytics' | 'compliance'

export const NAV: { id: View; label: string; section?: string }[] = [
  { id: 'overview', label: 'Overview', section: 'Monitor' },
  { id: 'issues', label: 'Issues' },
  { id: 'events', label: 'Events' },
  { id: 'devices', label: 'Devices' },
  { id: 'releases', label: 'Releases' },
  { id: 'artifacts', label: 'Artifacts', section: 'Debug' },
  { id: 'hardware', label: 'Hardware' },
  { id: 'boots', label: 'Boots' },
  { id: 'alerts', label: 'Alerts', section: 'Integrate' },
  { id: 'channels', label: 'Channels' },
  { id: 'relays', label: 'Relays' },
  { id: 'projects', label: 'Projects', section: 'Manage' },
  { id: 'dead', label: 'Dead jobs' },
  { id: 'audit', label: 'Audit' },
  { id: 'analytics', label: 'Analytics' },
  { id: 'compliance', label: 'Compliance' },
  { id: 'settings', label: 'Settings' },
]

export function isView(s: string | undefined): s is View {
  return !!s && NAV.some(n => n.id === s)
}
