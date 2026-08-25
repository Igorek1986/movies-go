import { NavLink } from 'react-router-dom'
import styles from './BottomNav.module.scss'

// Minimal stroke icons — currentColor so they pick up the active/inactive
// text color for free. No icon library in the project yet for just a
// handful of glyphs; exported so callers can compose their own item lists
// with them (or bring their own icons — `icon` just takes any ReactNode).
const iconProps = { width: 22, height: 22, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }

export function BackIcon() {
  return <svg {...iconProps}><path d="M15 18l-6-6 6-6" /></svg>
}
export function HomeIcon() {
  return <svg {...iconProps}><path d="M3 11l9-8 9 8" /><path d="M5 10v10h14V10" /></svg>
}
export function StarIcon() {
  return <svg {...iconProps}><path d="M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 21 12 17.77 5.82 21 7 14.14l-5-4.87 6.91-1.01L12 2z" /></svg>
}
export function HistoryIcon() {
  return <svg {...iconProps}><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3.5 2" /></svg>
}
export function CalendarIcon() {
  return <svg {...iconProps}><rect x="3" y="5" width="18" height="16" rx="2" /><path d="M3 10h18M8 3v4M16 3v4" /></svg>
}
export function SessionsIcon() {
  return <svg {...iconProps}><rect x="3" y="4" width="18" height="12" rx="2" /><path d="M8 20h8M12 16v4" /></svg>
}

// key -> icon, shared by BottomNav's own default set and the settings UI
// (SettingsBottomNav) so both stay in sync with BOTTOM_NAV_OPTIONS for free.
export const BOTTOM_NAV_ICONS: Record<string, React.ReactNode> = {
  back: <BackIcon />,
  home: <HomeIcon />,
  calendar: <CalendarIcon />,
  mine: <StarIcon />,
  history: <HistoryIcon />,
  sessions: <SessionsIcon />,
}

export interface BottomNavItem {
  key: string
  label: string
  icon: React.ReactNode
  // Route destination — renders as a NavLink and highlights while active.
  // Omit for a pure action button (e.g. "Назад").
  to?: string
  // Action handler. With `to`, runs as an extra side-effect on click (e.g.
  // resetting an expanded catalog category) rather than replacing navigation.
  onClick?: () => void
}

// Fixed mobile-only navigation bar (see .bottomNav — hidden on desktop via
// CSS). Configurable via `items`: add/remove/reorder buttons by changing the
// array passed in, no changes needed here. Used by Layout for the app-wide
// default set (Назад/Главная/Моё/История), but any page/context can render
// its own <BottomNav items={...}> with a different set if needed later.
export function BottomNav({ items }: { items: BottomNavItem[] }) {
  return (
    <nav className={styles.bottomNav} aria-label="Быстрая навигация">
      {items.map(item => item.to ? (
        <NavLink
          key={item.key}
          to={item.to}
          className={({ isActive }) => `${styles.bottomNavItem}${isActive ? ' ' + styles.bottomNavItemActive : ''}`}
          onClick={item.onClick}
        >
          {item.icon}
          <span>{item.label}</span>
        </NavLink>
      ) : (
        <button key={item.key} type="button" className={styles.bottomNavItem} onClick={item.onClick}>
          {item.icon}
          <span>{item.label}</span>
        </button>
      ))}
    </nav>
  )
}
