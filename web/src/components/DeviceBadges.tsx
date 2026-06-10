import { useState } from 'react'

export interface DeviceItem {
  id: number
  name: string
  timecode_count: number
}

export function DeviceBadges({ devices, onSelect }: {
  devices: DeviceItem[]
  onSelect?: (d: DeviceItem | null) => void
}) {
  const [selected, setSelected] = useState<number | null>(null)

  if (!devices.length) return <span style={{ color: '#555' }}>—</span>

  function toggle(d: DeviceItem) {
    const next = selected === d.id ? null : d.id
    setSelected(next)
    onSelect?.(next ? d : null)
  }

  return (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
      {devices.map(d => {
        const isSelected = selected === d.id
        return (
          <button
            key={d.id}
            onClick={e => { e.stopPropagation(); toggle(d) }}
            title={`${d.name}: ${d.timecode_count} таймкодов`}
            style={{
              fontSize: '0.75rem',
              background: isSelected ? 'rgba(80,180,120,0.35)' : 'rgba(80,180,120,0.12)',
              color: isSelected ? '#b8f0d0' : '#6dcc99',
              borderRadius: 4,
              padding: '2px 7px',
              border: `1px solid ${isSelected ? '#50b478' : 'rgba(80,180,120,0.3)'}`,
              whiteSpace: 'nowrap',
              cursor: 'pointer',
              transition: 'background 0.12s, border-color 0.12s',
            }}
          >
            {d.name}
          </button>
        )
      })}
    </div>
  )
}
