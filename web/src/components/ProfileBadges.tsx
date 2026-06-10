import { useState } from 'react'

export interface ProfileItem {
  id: string
  name: string
  timecode_count: number
}

export function ProfileBadges({ profiles, onSelect }: {
  profiles: ProfileItem[]
  onSelect?: (p: ProfileItem | null) => void
}) {
  const [selected, setSelected] = useState<string | null>(null)

  if (!profiles.length) return <span style={{ color: '#555' }}>—</span>

  function toggle(p: ProfileItem) {
    const next = selected === p.id ? null : p.id
    setSelected(next)
    onSelect?.(next ? p : null)
  }

  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
      {profiles.map(p => {
        const isSelected = selected === p.id
        return (
          <button
            key={p.id}
            onClick={e => { e.stopPropagation(); toggle(p) }}
            title={`${p.name || p.id}: ${p.timecode_count} таймкодов`}
            style={{
              fontSize: '0.75rem',
              background: isSelected ? 'rgba(74,144,226,0.35)' : 'rgba(74,144,226,0.12)',
              color: isSelected ? '#c8dcf8' : '#7ab4f5',
              borderRadius: 4,
              padding: '2px 7px',
              border: `1px solid ${isSelected ? '#4a90e2' : 'rgba(74,144,226,0.3)'}`,
              whiteSpace: 'nowrap',
              cursor: 'pointer',
              transition: 'background 0.12s, border-color 0.12s',
            }}
          >
            {p.name || p.id}
          </button>
        )
      })}
    </div>
  )
}
