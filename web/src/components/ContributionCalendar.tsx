import { useEffect, useMemo, useRef, useState } from 'react'
import styles from './ContributionCalendar.module.scss'

export interface DayActivity {
  date: string // YYYY-MM-DD
  count: number
  movies?: number
  episodes?: number
}

interface Props {
  data: DayActivity[]
  weeks?: number
  // Called with a day's date (YYYY-MM-DD) when a non-empty cell is clicked —
  // the page shows what was actually watched that day.
  onDayClick?: (date: string) => void
}

const WEEKDAY_LABELS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
const MONTH_LABELS = ['Янв', 'Фев', 'Мар', 'Апр', 'Май', 'Июн', 'Июл', 'Авг', 'Сен', 'Окт', 'Ноя', 'Дек']
// Genitive case ("3 мая", not "3 май") — for the tooltip's full date, unlike
// MONTH_LABELS above (nominative abbreviations, used in the grid header).
const MONTH_GENITIVE = ['января', 'февраля', 'марта', 'апреля', 'мая', 'июня', 'июля', 'августа', 'сентября', 'октября', 'ноября', 'декабря']

function toDateKey(d: Date): string {
  return d.toISOString().slice(0, 10)
}

export function formatDateRu(dateKey: string): string {
  const [y, m, d] = dateKey.split('-').map(Number)
  return `${d} ${MONTH_GENITIVE[m - 1]} ${y}`
}

// Russian plural forms: 1 просмотр, 2-4 просмотра, 5+ просмотров (with the
// usual 11-14 exception landing in the "many" bucket regardless of the last
// digit).
function pluralRu(n: number, [one, few, many]: [string, string, string]): string {
  const mod10 = n % 10
  const mod100 = n % 100
  if (mod10 === 1 && mod100 !== 11) return one
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return few
  return many
}

// Monday-based weekday index (0=Mon..6=Sun), unlike Date#getDay() (0=Sun).
function mondayIndex(d: Date): number {
  return (d.getDay() + 6) % 7
}

// GitHub-style contribution heatmap: `weeks` columns of 7 day-cells (Mon-Sun),
// ending on the current week. Intensity is bucketed by quantiles of the
// non-zero counts in `data`, not a fixed scale — so a light user (a handful
// of days with 1-2 items) still sees visible color variation instead of
// everything maxing out at the lightest bucket.
export default function ContributionCalendar({ data, weeks = 53, onDayClick }: Props) {
  const [hover, setHover] = useState<{ date: string; count: number; movies: number; episodes: number; x: number; y: number } | null>(null)
  const scrollerRef = useRef<HTMLDivElement>(null)

  const dataByDate = useMemo(() => {
    const m = new Map<string, DayActivity>()
    for (const d of data) m.set(d.date, d)
    return m
  }, [data])

  const thresholds = useMemo(() => {
    const nonZero = data.map(d => d.count).filter(c => c > 0).sort((a, b) => a - b)
    if (nonZero.length === 0) return [0, 0, 0, 0]
    const at = (p: number) => nonZero[Math.min(nonZero.length - 1, Math.floor(p * nonZero.length))]
    return [at(0.25), at(0.5), at(0.75), nonZero[nonZero.length - 1]]
  }, [data])

  function levelOf(count: number): 0 | 1 | 2 | 3 | 4 {
    if (count <= 0) return 0
    if (count <= thresholds[0]) return 1
    if (count <= thresholds[1]) return 2
    if (count <= thresholds[2]) return 3
    return 4
  }

  const { columns, monthLabels, total } = useMemo(() => {
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const end = new Date(today)
    end.setDate(end.getDate() + (6 - mondayIndex(end))) // end of current week (Sun)
    const start = new Date(end)
    start.setDate(start.getDate() - (weeks * 7 - 1))
    start.setDate(start.getDate() - mondayIndex(start)) // align to Monday

    const cols: { date: string; count: number; movies: number; episodes: number; inRange: boolean }[][] = []
    const cursor = new Date(start)
    const monthMarks: { label: string; col: number }[] = []
    let col = 0
    let lastMonth = -1
    let total = 0
    while (cursor <= end) {
      const week: { date: string; count: number; movies: number; episodes: number; inRange: boolean }[] = []
      for (let i = 0; i < 7; i++) {
        const key = toDateKey(cursor)
        const inRange = cursor >= start && cursor <= today
        const dayData = dataByDate.get(key)
        const count = dayData?.count ?? 0
        if (inRange) total += count
        // Label a column as soon as its month first appears in the grid —
        // checked on every day, not just Monday: a month whose 1st lands
        // mid-week (the common case) would otherwise never get labeled
        // until the FOLLOWING Monday, which can fall outside the `weeks`
        // window entirely (e.g. the last column spans Aug 31 - Sep 6, so
        // "Сен" never got its own Monday inside a 53-week grid ending there).
        if ((i === 0 || cursor.getDate() === 1) && cursor.getMonth() !== lastMonth && cursor <= end) {
          lastMonth = cursor.getMonth()
          monthMarks.push({ label: MONTH_LABELS[lastMonth], col })
        }
        week.push({
          date: key,
          count,
          movies: dayData?.movies ?? 0,
          episodes: dayData?.episodes ?? 0,
          inRange: inRange && cursor <= today,
        })
        cursor.setDate(cursor.getDate() + 1)
      }
      cols.push(week)
      col++
    }
    return { columns: cols, monthLabels: monthMarks, total }
  }, [dataByDate, weeks])

  // Weeks run oldest→newest left-to-right, so the current date is always the
  // rightmost column — without this the grid opens scrolled to the far
  // left (a year ago) and the user has to scroll right just to see today.
  useEffect(() => {
    const el = scrollerRef.current
    if (el) el.scrollLeft = el.scrollWidth
  }, [columns])

  return (
    <div className={styles.wrap}>
      <div className={styles.header}>
        <span className={styles.total}>{total.toLocaleString('ru')} просмотрено за последний год</span>
        <div className={styles.legend}>
          <span>Меньше</span>
          {[0, 1, 2, 3, 4].map(l => (
            <span key={l} className={styles.cell} data-level={l} />
          ))}
          <span>Больше</span>
        </div>
      </div>
      <div className={styles.scroller} ref={scrollerRef}>
        <div className={styles.grid}>
          <div className={styles.weekdayCol}>
            {WEEKDAY_LABELS.map((w, i) => (
              <span key={w} className={styles.weekdayLabel}>{i % 2 === 0 ? w : ''}</span>
            ))}
          </div>
          <div className={styles.weeksCol}>
            <div className={styles.months}>
              {monthLabels.map((m, i) => {
                const isLast = i === monthLabels.length - 1
                return (
                  <span
                    key={i}
                    className={`${styles.monthLabel}${isLast ? ' ' + styles.monthLabelLast : ''}`}
                    style={{ gridColumnStart: m.col + 1 }}
                  >
                    {m.label}
                  </span>
                )
              })}
            </div>
            <div className={styles.weeks}>
              {columns.map((week, wi) => (
                <div key={wi} className={styles.week}>
                  {week.map((day, di) => (
                    day.inRange ? (
                      <button
                        key={di}
                        type="button"
                        className={styles.cell}
                        data-level={levelOf(day.count)}
                        aria-label={`${day.date}: ${day.count}`}
                        onMouseEnter={e => {
                          const r = (e.target as HTMLElement).getBoundingClientRect()
                          setHover({ date: day.date, count: day.count, movies: day.movies, episodes: day.episodes, x: r.left + r.width / 2, y: r.top })
                        }}
                        onFocus={e => {
                          const r = (e.target as HTMLElement).getBoundingClientRect()
                          setHover({ date: day.date, count: day.count, movies: day.movies, episodes: day.episodes, x: r.left + r.width / 2, y: r.top })
                        }}
                        onMouseLeave={() => setHover(null)}
                        onBlur={() => setHover(null)}
                        onClick={() => day.count > 0 && onDayClick?.(day.date)}
                      />
                    ) : <span key={di} className={styles.cellEmpty} />
                  ))}
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>
      {hover && (
        <div className={styles.tooltip} style={{ left: hover.x, top: hover.y }}>
          <div>{formatDateRu(hover.date)} — <strong>{hover.count}</strong> {pluralRu(hover.count, ['просмотр', 'просмотра', 'просмотров'])}</div>
          {hover.movies > 0 && <div className={styles.tooltipLine}>Фильмы: {hover.movies}</div>}
          {hover.episodes > 0 && <div className={styles.tooltipLine}>Эпизоды: {hover.episodes}</div>}
        </div>
      )}
    </div>
  )
}
