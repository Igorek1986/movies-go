import { useEffect, useMemo, useRef, useState } from 'react'
import styles from './DailyChart.module.scss'

export interface DailyPoint {
  date: string
  plays: number
  viewers: number
  cards: number
}

function fmtDay(date: string): string {
  const [, m, d] = date.split('-')
  return `${d}.${m}`
}

interface Props {
  daily: DailyPoint[]
  title: string
  // When provided, the chart acts as a date filter: clicking a bar reports its
  // date (or null when the same bar is clicked again). Without these it just
  // works as a passive readout.
  selected?: string | null
  onSelect?: (date: string | null) => void
}

// Per-day play dynamics. Bars and their date labels share one flex column, so
// labels stay aligned under their bar. Each column has a min width and the
// chart scrolls horizontally when it doesn't fit (narrow/tablet screens), so
// every date label can be shown without truncation; on wide screens the
// columns stretch to fill the width and no scrolling is needed.
//
// Hover (mouse) previews a day's numbers in the readout; selection (click or
// tap) pins a day and drives the optional filter. Hover and selection are kept
// in separate state and on separate events (mouse vs pointerType) so a single
// touch can't fire both at once.
export default function DailyChart({ daily, title, selected, onSelect }: Props) {
  const [hover, setHover] = useState<number | null>(null)
  const [internalSel, setInternalSel] = useState<string | null>(null)
  const chartRef = useRef<HTMLDivElement>(null)

  // Newest day first (left). With horizontal scroll this keeps the most recent
  // dates in view without scrolling.
  const days = useMemo(() => [...daily].reverse(), [daily])

  // When the chart overflows, translate vertical wheel into horizontal scroll
  // so the page doesn't scroll away while the pointer is over the chart. Needs
  // a non-passive native listener to be able to preventDefault.
  useEffect(() => {
    const el = chartRef.current
    if (!el) return
    const onWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) return
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return
      el.scrollLeft += e.deltaY
      e.preventDefault()
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  }, [])

  if (days.length === 0) return null

  const controlled = onSelect != null
  const sel = controlled ? selected ?? null : internalSel
  const setSel = (date: string | null) => (controlled ? onSelect!(date) : setInternalSel(date))
  const selIdx = sel != null ? days.findIndex(d => d.date === sel) : -1

  const maxPlays = days.reduce((m, d) => Math.max(m, d.plays), 0) || 1
  const n = days.length
  const active = hover != null ? days[hover] : selIdx >= 0 ? days[selIdx] : null

  function toggle(i: number) {
    const date = daily[i].date
    setSel(sel === date ? null : date)
  }

  return (
    <div className={styles.chartCard}>
      <div className={styles.chartHead}>
        <p className={styles.chartTitle}>{title}</p>
        <span className={styles.readout}>
          {active
            ? `${fmtDay(active.date)}: ${active.plays} просм. · ${active.viewers} зрит. · ${active.cards} карт.`
            : `${n} дн.`}
        </span>
      </div>
      <div className={styles.chart} ref={chartRef}>
        {days.map((d, i) => (
          <div
            key={d.date}
            className={`${styles.col} ${selIdx === i ? styles.colActive : ''}`}
            onPointerEnter={e => { if (e.pointerType === 'mouse') setHover(i) }}
            onPointerLeave={e => { if (e.pointerType === 'mouse') setHover(null) }}
            onClick={() => toggle(i)}
            title={`${fmtDay(d.date)}: ${d.plays} просмотров, ${d.viewers} зрителей, ${d.cards} карточек`}
          >
            <div className={styles.barWrap}>
              <div className={styles.barFill} style={{ height: `${(d.plays / maxPlays) * 100}%` }} />
            </div>
            <div className={styles.barLabel}>{fmtDay(d.date)}</div>
          </div>
        ))}
      </div>
    </div>
  )
}
