import { useEffect, useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { isPushSupported, getPushStatus, subscribeToPush, unsubscribeFromPush } from '@/utils/push'
import { getGridCols } from '@/utils/scrollNav'
import styles from './CalendarPage.module.scss'

interface CalendarEpisode {
  card_id: string
  tmdb_id: number
  title: string
  poster_path: string | null
  season: number
  episode: number
  air_date: string // YYYY-MM-DD
  episode_name: string
}

interface CalendarResponse {
  year: number
  month: number
  episodes: CalendarEpisode[]
}

const WEEKDAYS = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс']
const MONTH_NAMES = [
  'Январь', 'Февраль', 'Март', 'Апрель', 'Май', 'Июнь',
  'Июль', 'Август', 'Сентябрь', 'Октябрь', 'Ноябрь', 'Декабрь',
]

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n)
}

function todayStr(): string {
  const d = new Date()
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

// Monday-first grid of a month: always 6 full weeks (42 days), with leading/trailing
// days from adjacent months filling the gaps — a stable grid size regardless of how
// the month's weekdays line up.
function buildGrid(year: number, month: number): { date: string; inMonth: boolean }[] {
  const first = new Date(year, month - 1, 1)
  const startOffset = (first.getDay() + 6) % 7 // Mon=0..Sun=6
  const cursor = new Date(year, month - 1, 1 - startOffset)

  const days: { date: string; inMonth: boolean }[] = []
  for (let i = 0; i < 42; i++) {
    days.push({
      date: `${cursor.getFullYear()}-${pad2(cursor.getMonth() + 1)}-${pad2(cursor.getDate())}`,
      inMonth: cursor.getMonth() === month - 1,
    })
    cursor.setDate(cursor.getDate() + 1)
  }
  return days
}

export default function CalendarPage() {
  const navigate = useNavigate()
  const { activeDevice, activeProfile, loaded } = useActiveProfile()

  const now = new Date()
  const [year, setYear] = useState(now.getFullYear())
  const [month, setMonth] = useState(now.getMonth() + 1) // 1-12
  const [episodes, setEpisodes] = useState<CalendarEpisode[] | null>(null)
  const [error, setError] = useState(false)
  const [selectedDate, setSelectedDate] = useState<string | null>(todayStr())
  const [pushSubscribed, setPushSubscribed] = useState(false)
  const [pushBusy, setPushBusy] = useState(false)

  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  const load = useCallback(() => {
    if (!loaded || !token) return
    setEpisodes(null)
    setError(false)
    const params = new URLSearchParams({ token, profile_id: profileId, year: String(year), month: String(month) })
    fetch(`/calendar?${params}`)
      .then(r => r.ok ? r.json() : Promise.reject())
      .then((data: CalendarResponse) => setEpisodes(data.episodes || []))
      .catch(() => { setError(true); setEpisodes([]) })
  }, [loaded, token, profileId, year, month])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (!loaded || !token) return
    getPushStatus().then(setPushSubscribed)
  }, [loaded, token, profileId])

  async function togglePush() {
    if (!token || pushBusy) return
    setPushBusy(true)
    try {
      const ok = pushSubscribed ? await unsubscribeFromPush(token) : await subscribeToPush(token, profileId)
      if (ok) setPushSubscribed(!pushSubscribed)
    } finally {
      setPushBusy(false)
    }
  }

  async function sendTestPush() {
    if (!token) return
    const params = new URLSearchParams({ token, profile_id: profileId })
    await fetch(`/push/test?${params}`, { method: 'POST' })
  }

  // Re-selecting the day panel on month navigation: land on today if the viewed
  // month is the current one, otherwise clear the selection.
  useEffect(() => {
    const d = new Date()
    setSelectedDate(d.getFullYear() === year && d.getMonth() + 1 === month ? todayStr() : null)
  }, [year, month])

  function goMonth(delta: number) {
    let m = month + delta
    let y = year
    if (m < 1) { m = 12; y -= 1 }
    else if (m > 12) { m = 1; y += 1 }
    setMonth(m)
    setYear(y)
  }

  function goToday() {
    const d = new Date()
    setYear(d.getFullYear())
    setMonth(d.getMonth() + 1)
    setSelectedDate(todayStr())
  }

  // Keyboard navigation: arrow keys move focus across the day grid, then down
  // into the selected day's episode list (mirrors the card-grid nav on other
  // pages — see HistoryPage/CatalogPage/MediaLibraryPage).
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const tag = (document.activeElement as HTMLElement)?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'select' || tag === 'textarea') return

      if (e.key === 'Backspace') { navigate(-1); return }

      const cells = Array.from(document.querySelectorAll<HTMLElement>('[data-cal-cell]'))
      const dayItems = Array.from(document.querySelectorAll<HTMLElement>('[data-cal-day-item]'))
      const focused = document.activeElement as HTMLElement
      const cellIdx = cells.indexOf(focused)
      const dayItemIdx = dayItems.indexOf(focused)

      if (e.key === 'Enter' && (cellIdx !== -1 || dayItemIdx !== -1)) {
        e.preventDefault()
        focused.click()
        return
      }

      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
      e.preventDefault()

      if (dayItemIdx !== -1) {
        if (e.key === 'ArrowDown') {
          dayItems[Math.min(dayItemIdx + 1, dayItems.length - 1)].focus()
        } else if (e.key === 'ArrowUp') {
          if (dayItemIdx === 0) {
            (cells.find(c => c.dataset.date === selectedDate) ?? cells[cells.length - 1])?.focus()
          } else {
            dayItems[dayItemIdx - 1].focus()
          }
        }
        return
      }

      if (!cells.length) return

      if (cellIdx === -1) {
        (cells.find(c => c.dataset.date === selectedDate) ?? cells[0]).focus()
        return
      }

      const cols = getGridCols(cells)
      let next = -1
      if (e.key === 'ArrowRight') next = Math.min(cellIdx + 1, cells.length - 1)
      else if (e.key === 'ArrowLeft') next = Math.max(cellIdx - 1, 0)
      else if (e.key === 'ArrowUp') next = Math.max(cellIdx - cols, 0)
      else if (e.key === 'ArrowDown') {
        if (cellIdx >= cells.length - cols) {
          dayItems[0]?.focus()
          return
        }
        next = Math.min(cellIdx + cols, cells.length - 1)
      }

      if (next !== -1 && next !== cellIdx) {
        cells[next].focus()
      }
    }

    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [navigate, selectedDate])

  const byDate = new Map<string, CalendarEpisode[]>()
  for (const ep of episodes ?? []) {
    const list = byDate.get(ep.air_date)
    if (list) list.push(ep)
    else byDate.set(ep.air_date, [ep])
  }

  const today = todayStr()
  const grid = buildGrid(year, month)
  const selectedEpisodes = selectedDate ? (byDate.get(selectedDate) ?? []) : []

  function openCard(cardId: string) {
    navigate(`/card/${cardId}`, { state: { backUrl: '/calendar' } })
  }

  return (
    <Layout>
      <div className={styles.page}>
        <div className={styles.header}>
          <h1 className={styles.title}>Календарь</h1>
          <div className={styles.monthNav}>
            <button className={styles.navBtn} onClick={() => goMonth(-1)} aria-label="Предыдущий месяц">‹</button>
            <span className={styles.monthLabel}>{MONTH_NAMES[month - 1]} {year}</span>
            <button className={styles.navBtn} onClick={() => goMonth(1)} aria-label="Следующий месяц">›</button>
            <button className={styles.todayBtn} onClick={goToday}>Сегодня</button>
            {isPushSupported() && (
              <button
                className={`${styles.bellBtn}${pushSubscribed ? ' ' + styles.bellOn : ''}`}
                onClick={togglePush}
                disabled={pushBusy}
                title={pushSubscribed ? 'Уведомления о новых сериях включены' : 'Включить уведомления о новых сериях'}
              >
                {pushSubscribed ? '🔔' : '🔕'}
              </button>
            )}
            {pushSubscribed && (
              <button className={styles.todayBtn} onClick={sendTestPush}>Тест push</button>
            )}
          </div>
        </div>

        {error && <p className={styles.errorMsg}>Не удалось загрузить расписание</p>}

        <div className={styles.weekdays}>
          {WEEKDAYS.map(d => <div key={d} className={styles.weekday}>{d}</div>)}
        </div>

        <div className={styles.grid} data-row-id="calendar-grid">
          {grid.map(({ date, inMonth }) => {
            const dayEpisodes = byDate.get(date) ?? []
            const dayNum = Number(date.slice(8, 10))
            const isToday = date === today
            const isSelected = date === selectedDate
            return (
              <button
                key={date}
                className={`${styles.cell}${inMonth ? '' : ' ' + styles.outMonth}${isToday ? ' ' + styles.today : ''}${isSelected ? ' ' + styles.selected : ''}${dayEpisodes.length ? ' ' + styles.hasEvents : ''}`}
                data-cal-cell
                data-nav-item
                data-date={date}
                onClick={() => dayEpisodes.length && setSelectedDate(isSelected ? null : date)}
              >
                <span className={styles.dayNum}>{dayNum}</span>
                {dayEpisodes.length > 0 && (
                  <span className={styles.dayChips}>
                    {dayEpisodes.slice(0, 2).map((ep, i) => {
                      const thumb = posterUrl(ep.poster_path, 'w92')
                      return thumb
                        ? <img key={i} className={styles.chip} src={thumb} alt="" title={ep.title} loading="lazy" />
                        : <span key={i} className={`${styles.chip} ${styles.chipPlaceholder}`} title={ep.title} />
                    })}
                    {dayEpisodes.length > 2 && <span className={styles.chipMore}>+{dayEpisodes.length - 2}</span>}
                  </span>
                )}
              </button>
            )
          })}
        </div>

        {episodes === null && !error && <p className={styles.loadingMsg}>Загрузка…</p>}
        {episodes !== null && episodes.length === 0 && !error && (
          <p className={styles.emptyMsg}>В этом месяце новых серий по отслеживаемым сериалам нет</p>
        )}

        {selectedDate && episodes !== null && (
          <div className={styles.dayPanel}>
            <p className={styles.dayPanelTitle}>{selectedDate.split('-').reverse().join('.')}</p>
            {selectedEpisodes.length === 0 && <p className={styles.dayEmpty}>Нет серий</p>}
            <div className={styles.dayList} data-row-id="calendar-day-list">
              {selectedEpisodes.map((ep, i) => {
                const poster = posterUrl(ep.poster_path)
                return (
                <div
                  key={i}
                  className={styles.dayItem}
                  data-cal-day-item
                  data-nav-item
                  tabIndex={0}
                  role="button"
                  onClick={() => openCard(ep.card_id)}
                >
                  {poster
                    ? <img className={styles.dayPoster} src={poster} alt={ep.title} loading="lazy" />
                    : <div className={styles.dayPosterPlaceholder} />
                  }
                  <div className={styles.dayItemInfo}>
                    <p className={styles.dayItemTitle}>{ep.title}</p>
                    <p className={styles.dayItemMeta}>
                      S{pad2(ep.season)}E{pad2(ep.episode)}{ep.episode_name ? ` · ${ep.episode_name}` : ''}
                    </p>
                  </div>
                </div>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
