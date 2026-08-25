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
export function SearchIcon() {
  return <svg {...iconProps}><circle cx="11" cy="11" r="7" /><path d="M21 21l-4.3-4.3" /></svg>
}
export function SettingsIcon() {
  return <svg {...iconProps}><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-1.87-.34 1.7 1.7 0 0 0-1.04 1.56V21a2 2 0 1 1-4 0v-.09A1.7 1.7 0 0 0 8.96 19.4a1.7 1.7 0 0 0-1.87.34l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.7 1.7 0 0 0 .34-1.87 1.7 1.7 0 0 0-1.56-1.04H3a2 2 0 1 1 0-4h.09A1.7 1.7 0 0 0 4.6 8.96a1.7 1.7 0 0 0-.34-1.87l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.7 1.7 0 0 0 1.87.34H9a1.7 1.7 0 0 0 1.04-1.56V3a2 2 0 1 1 4 0v.09a1.7 1.7 0 0 0 1.04 1.56 1.7 1.7 0 0 0 1.87-.34l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87V9a1.7 1.7 0 0 0 1.56 1.04H21a2 2 0 1 1 0 4h-.09a1.7 1.7 0 0 0-1.56 1.04Z" /></svg>
}

// key -> icon, shared by BottomNav's own default set and the settings UI
// (SettingsBottomNav) so both stay in sync with BOTTOM_NAV_OPTIONS for free.
export const BOTTOM_NAV_ICONS: Record<string, React.ReactNode> = {
  back: <BackIcon />,
  home: <HomeIcon />,
  search: <SearchIcon />,
  calendar: <CalendarIcon />,
  mine: <StarIcon />,
  history: <HistoryIcon />,
  sessions: <SessionsIcon />,
  settings: <SettingsIcon />,
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

// Fixed mobile/tablet navigation bar (see .bottomNav — hidden on desktop via
// CSS). Configurable via `items`: add/remove/reorder buttons by changing the
// array passed in, no changes needed here. Used by Layout for the app-wide
// default set (Назад/Главная/Моё/История), but any page/context can render
// its own <BottomNav items={...}> with a different set if needed later.
// `position` picks the docked edge ("bottom", default, or "right"/"left" —
// a vertical strip, mainly meant for the tablet width range). `forceShow`
// and `navRef`/`onBlur` are for the desktop keyboard-summoned panel (see
// Layout.tsx) — irrelevant to the normal tablet/mobile CSS-driven rendering.
export function BottomNav({
  items, position = 'bottom', forceShow, navRef, onBlur,
}: {
  items: BottomNavItem[]
  position?: 'bottom' | 'right' | 'left'
  forceShow?: boolean
  navRef?: React.Ref<HTMLElement>
  onBlur?: React.FocusEventHandler<HTMLElement>
}) {
  const navClass = `${styles.bottomNav}${position !== 'bottom' ? ' ' + styles[position] : ''}${forceShow ? ' ' + styles.forceShow : ''}`
  return (
    <nav ref={navRef} className={navClass} aria-label="Быстрая навигация" onBlur={onBlur}>
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
