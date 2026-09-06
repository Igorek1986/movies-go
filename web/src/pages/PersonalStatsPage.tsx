import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import Layout from '@/components/Layout'
import ContributionCalendar, { formatDateRu, type DayActivity } from '@/components/ContributionCalendar'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { posterUrl, tmdbUrl } from '@/utils/poster'
import { getGridCols, focusTopNavActive } from '@/utils/scrollNav'
import styles from './PersonalStatsPage.module.scss'

interface GenreCount {
  name: string
  count: number
}

interface ActorCount {
  person_id: number
  name: string
  profile_path: string
  count: number
}

interface ProfileStats {
  movies_watched: number
  series_completed: number
  series_watching: number
  planned_movies: number
  planned_series: number
  stopped: number
  favorites: number
  episodes_watched: number
  watch_time_minutes: number
  remaining_minutes: number
  current_streak: number
  longest_streak: number
  favorite_weekday: number // 0=Mon..6=Sun, -1 = no data
  calendar: DayActivity[]
  top_genres: GenreCount[]
  top_actors: ActorCount[]
}

// Matches toMediaItem's (backend) shape for /api/stats/personal/list — the
// same fields MediaLibraryPage's LibraryItem uses, plus the stats-only
// extras handleProfileStatsList merges in for kind=movies/series.
interface StatsListItem {
  id: number
  media_type: string
  title: string
  name: string
  poster_path: string | null
  release_date: string
  first_air_date: string
  percent?: number
  view_count?: number
  watched_episodes?: number
  total_episodes?: number
}

type ExpandedKind = 'movies' | 'series' | 'planned_movies' | 'planned_series' | 'stopped' | 'favorites'

const EXPANDED_TITLES: Record<ExpandedKind, string> = {
  movies: 'Просмотренные фильмы',
  series: 'Сериалы (смотрю + завершено)',
  planned_movies: 'Буду смотреть — фильмы',
  planned_series: 'Буду смотреть — сериалы',
  stopped: 'Брошено',
  favorites: 'В избранном',
}

function cardIdOf(item: StatsListItem): string {
  return `${item.id}_${item.media_type}`
}

const WEEKDAY_NAMES = ['Понедельник', 'Вторник', 'Среда', 'Четверг', 'Пятница', 'Суббота', 'Воскресенье']

// Genre names come from TMDB with inconsistent casing ("комедия" vs "НФ и
// Фэнтези") — capitalize only the first letter, don't touch the rest (a
// blanket text-transform: capitalize would also uppercase "и"/"Фэнтези").
function capitalizeFirst(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

// Returns a separate `sub` line for the day count instead of folding it into
// one long string — "3 811 ч (158,8 дн.)" as a single value wrapped the tile
// onto three lines and dwarfed every other tile's height.
function formatWatchTime(minutes: number): { main: string; sub?: string } {
  const hours = Math.round(minutes / 60)
  if (hours < 24) return { main: `${hours.toLocaleString('ru')} ч` }
  const days = Math.round((hours / 24) * 10) / 10
  return { main: `${hours.toLocaleString('ru')} ч`, sub: `≈ ${days.toLocaleString('ru')} дн.` }
}

const STATS_LIST_PAGE_SIZE = 30

interface StatsListPage {
  items: StatsListItem[]
  totalPages: number
  // Only present for kind=series — aggregated server-side over the FULL
  // card_id set, not just this page's rows (see handleProfileStatsList).
  watchedEpisodesTotal?: number
  totalEpisodesTotal?: number
}

// Matches store.DayItem (backend) — what was watched on one specific day.
interface DayItem {
  card_id: string
  media_type: string
  title: string
  poster_path: string
  season?: number
  episode?: number
  episode_name?: string
}

async function fetchDayItems(token: string, profileId: string, date: string): Promise<DayItem[]> {
  const params = new URLSearchParams({ token, profile_id: profileId, date })
  const res = await fetch(`/api/stats/personal/day?${params}`)
  if (!res.ok) return []
  const data = await res.json()
  return Array.isArray(data.results) ? data.results : []
}

function DayItemCard({ item, onOpen }: { item: DayItem; onOpen: () => void }) {
  const url = posterUrl(item.poster_path)
  const hasEpisode = typeof item.season === 'number' && typeof item.episode === 'number'
  return (
    <button type="button" className={styles.statsCard} data-nav-item onClick={onOpen}>
      <div className={styles.statsCardPoster}>
        {url
          ? <img src={url} alt={item.title} loading="lazy" />
          : <div className={styles.posterPlaceholder}>Нет постера</div>}
        {hasEpisode && (
          <span className={styles.percentBadge}>
            S{String(item.season).padStart(2, '0')}E{String(item.episode).padStart(2, '0')}
          </span>
        )}
      </div>
      <p className={styles.statsCardTitle}>{item.title}</p>
      {hasEpisode && item.episode_name && <p className={styles.dayItemEpisode}>{item.episode_name}</p>}
    </button>
  )
}

async function fetchStatsList(kind: ExpandedKind, token: string, profileId: string, page: number): Promise<StatsListPage> {
  const params = new URLSearchParams({ token, profile_id: profileId, kind, page: String(page), per_page: String(STATS_LIST_PAGE_SIZE) })
  const res = await fetch(`/api/stats/personal/list?${params}`)
  if (!res.ok) return { items: [], totalPages: 1 }
  const data = await res.json()
  return {
    items: Array.isArray(data.results) ? data.results : [],
    totalPages: data.total_pages ?? 1,
    watchedEpisodesTotal: data.watched_episodes_total,
    totalEpisodesTotal: data.total_episodes_total,
  }
}

function StatsCard({ item, kind, onOpen }: { item: StatsListItem; kind: ExpandedKind; onOpen: () => void }) {
  const url = posterUrl(item.poster_path)
  const title = item.title || item.name
  const showRewatch = kind === 'movies' && typeof item.view_count === 'number' && item.view_count > 1
  const showPercent = kind === 'movies' && typeof item.percent === 'number' && item.percent > 0 && item.percent < 100
  const showProgress = kind === 'series' && typeof item.total_episodes === 'number' && item.total_episodes > 0
  return (
    <button type="button" className={styles.statsCard} data-nav-item onClick={onOpen}>
      <div className={styles.statsCardPoster}>
        {url
          ? <img src={url} alt={title} loading="lazy" />
          : <div className={styles.posterPlaceholder}>Нет постера</div>}
        {showRewatch && <span className={styles.rewatchBadge}>×{item.view_count}</span>}
        {showPercent && <span className={styles.percentBadge}>{Math.round(item.percent!)}%</span>}
        {showProgress && (
          <>
            <span className={styles.progressLabel}>{item.watched_episodes ?? 0}/{item.total_episodes}</span>
            <div className={styles.progressTrack}>
              <div
                className={styles.progressFill}
                style={{ width: `${Math.min(100, ((item.watched_episodes ?? 0) / item.total_episodes!) * 100)}%` }}
              />
            </div>
          </>
        )}
      </div>
      <p className={styles.statsCardTitle}>{title}</p>
    </button>
  )
}

export default function PersonalStatsPage() {
  const navigate = useNavigate()
  const { activeDevice, activeProfile, loaded } = useActiveProfile()
  const [stats, setStats] = useState<ProfileStats | null>(null)
  const [loading, setLoading] = useState(true)

  const [expanded, setExpanded] = useState<ExpandedKind | null>(null)
  const [itemsCache, setItemsCache] = useState<Partial<Record<ExpandedKind, StatsListItem[]>>>({})
  const [pageCache, setPageCache] = useState<Partial<Record<ExpandedKind, number>>>({})
  const [hasMoreCache, setHasMoreCache] = useState<Partial<Record<ExpandedKind, boolean>>>({})
  const [seriesEpisodeTotals, setSeriesEpisodeTotals] = useState<{ watched: number; total: number } | null>(null)
  const [expandedLoading, setExpandedLoading] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const lastTileRef = useRef<HTMLButtonElement | null>(null)
  const sentinelRef = useRef<HTMLDivElement | null>(null)

  const [selectedDay, setSelectedDay] = useState<string | null>(null)
  const [dayItems, setDayItems] = useState<DayItem[] | null>(null)
  const [dayItemsLoading, setDayItemsLoading] = useState(false)

  const token = activeDevice?.token ?? ''
  const profileId = activeProfile?.profile_id ?? ''

  useEffect(() => {
    if (!loaded) return
    if (!token) { setStats(null); setLoading(false); return }
    setLoading(true)
    const params = new URLSearchParams({ token, profile_id: profileId })
    fetch(`/api/stats/personal?${params}`)
      .then(res => res.ok ? res.json() : null)
      .then(data => setStats(data))
      .finally(() => setLoading(false))
    // Switching profile invalidates any already-fetched detail lists.
    setExpanded(null)
    setItemsCache({})
    setPageCache({})
    setHasMoreCache({})
    setSeriesEpisodeTotals(null)
    setSelectedDay(null)
    setDayItems(null)
  }, [loaded, token, profileId])

  async function openDay(date: string) {
    if (selectedDay === date) { setSelectedDay(null); setDayItems(null); return }
    setSelectedDay(date)
    setDayItemsLoading(true)
    setDayItems(await fetchDayItems(token, profileId, date))
    setDayItemsLoading(false)
  }

  // Loads one page of a list: page 1 (reset) replaces the cache, later pages
  // append — same pattern as MediaLibraryPage's search infinite scroll
  // (loadSearchPage), which every kind here shares since a profile can have
  // way more than one page's worth of movies/series/planned/favorites.
  async function loadListPage(kind: ExpandedKind, page: number, reset: boolean) {
    if (reset) setExpandedLoading(true); else setLoadingMore(true)
    const { items, totalPages, watchedEpisodesTotal, totalEpisodesTotal } = await fetchStatsList(kind, token, profileId, page)
    setItemsCache(prev => ({ ...prev, [kind]: reset ? items : [...(prev[kind] ?? []), ...items] }))
    setPageCache(prev => ({ ...prev, [kind]: page }))
    setHasMoreCache(prev => ({ ...prev, [kind]: totalPages > page }))
    // Server-aggregated over the whole list regardless of page — see
    // handleProfileStatsList — so this is safe to just overwrite each time,
    // unlike itemsCache which has to append.
    if (kind === 'series' && typeof watchedEpisodesTotal === 'number' && typeof totalEpisodesTotal === 'number') {
      setSeriesEpisodeTotals({ watched: watchedEpisodesTotal, total: totalEpisodesTotal })
    }
    if (reset) setExpandedLoading(false); else setLoadingMore(false)
  }

  function toggleExpanded(kind: ExpandedKind, btn: HTMLButtonElement) {
    if (expanded === kind) { setExpanded(null); return }
    lastTileRef.current = btn
    setExpanded(kind)
    if (pageCache[kind]) return
    loadListPage(kind, 1, true)
  }

  // Infinite scroll: load the next page once the sentinel below the grid
  // scrolls into view.
  useEffect(() => {
    const sentinel = sentinelRef.current
    if (!sentinel || !expanded) return
    const hasMore = hasMoreCache[expanded]
    const observer = new IntersectionObserver(entries => {
      if (entries[0].isIntersecting && hasMore && !loadingMore && !expandedLoading) {
        loadListPage(expanded, (pageCache[expanded] ?? 1) + 1, false)
      }
    }, { rootMargin: '200px' })
    observer.observe(sentinel)
    return () => observer.disconnect()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [expanded, hasMoreCache, loadingMore, expandedLoading, pageCache])

  // Basic arrow-key navigation for this page only (no site-wide focus engine
  // yet — see project backlog #70). Follows the same recipe as
  // CalendarPage.tsx: a page-level keydown effect moving focus between
  // [data-nav-item] elements inside named data-row-id regions. Tiles/cards/
  // actor links are all native <button>/<a> here, so Enter/Space activation
  // is free — only arrow movement and closing the expanded list need code.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const tag = (document.activeElement as HTMLElement)?.tagName?.toLowerCase()
      if (tag === 'input' || tag === 'select' || tag === 'textarea') return

      // Layout.tsx skips its own site-wide Backspace-navigates-back handling
      // on this page (see its onKeyDown comment) specifically so this page
      // can decide for itself — same convention as CatalogPage: collapse the
      // expanded list first, only navigate back once there's nothing left to
      // collapse.
      if (e.key === 'Backspace') {
        e.preventDefault()
        if (selectedDay) { setSelectedDay(null); setDayItems(null) }
        else if (expanded) { setExpanded(null); lastTileRef.current?.focus() }
        else navigate(-1)
        return
      }
      if (e.key === 'Escape') {
        if (selectedDay) {
          e.preventDefault()
          setSelectedDay(null)
          setDayItems(null)
        } else if (expanded) {
          e.preventDefault()
          setExpanded(null)
          lastTileRef.current?.focus()
        }
        return
      }

      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return
      const focused = document.activeElement as HTMLElement | null
      if (!focused) return

      // Focus starts outside this page's own regions entirely — on the top
      // nav (Layout.tsx deliberately leaves ArrowDown from there alone, see
      // its onKeyDown comment, so each page decides what "into the page"
      // means) or nowhere in particular. Same fallback as CatalogPage's
      // "focus isn't on a card → jump to the first one".
      if (!focused.closest('[data-row-id="stats-tiles"], [data-row-id="stats-expanded"], [data-row-id="stats-day"], [data-row-id="stats-actors"]')) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          document.querySelector<HTMLElement>('[data-row-id="stats-tiles"] [data-nav-item]')?.focus()
        }
        return
      }

      const actorRow = focused.closest<HTMLElement>('[data-row-id="stats-actors"]')
      if (actorRow) {
        const cards = Array.from(actorRow.querySelectorAll<HTMLElement>('[data-nav-item]'))
        const idx = cards.indexOf(focused)
        if (e.key === 'ArrowLeft' && idx > 0) { e.preventDefault(); cards[idx - 1].focus() }
        else if (e.key === 'ArrowRight' && idx < cards.length - 1) { e.preventDefault(); cards[idx + 1].focus() }
        else if (e.key === 'ArrowUp') {
          e.preventDefault()
          const target = document.querySelector<HTMLElement>(
            `[data-row-id="${expanded ? 'stats-expanded' : 'stats-tiles'}"] [data-nav-item]`,
          )
          target?.focus()
        }
        return
      }

      const expandedRow = focused.closest<HTMLElement>('[data-row-id="stats-expanded"], [data-row-id="stats-day"]')
      if (expandedRow) {
        const cards = Array.from(expandedRow.querySelectorAll<HTMLElement>('[data-nav-item]'))
        const idx = cards.indexOf(focused)
        if (idx === -1) return
        const cols = getGridCols(cards)
        e.preventDefault()
        if (e.key === 'ArrowRight') cards[Math.min(idx + 1, cards.length - 1)]?.focus()
        else if (e.key === 'ArrowLeft') cards[Math.max(idx - 1, 0)]?.focus()
        else if (e.key === 'ArrowUp') {
          if (idx < cols) document.querySelector<HTMLElement>('[data-row-id="stats-tiles"] [data-nav-item]')?.focus()
          else cards[idx - cols]?.focus()
        } else if (e.key === 'ArrowDown') {
          cards[Math.min(idx + cols, cards.length - 1)]?.focus()
        }
        return
      }

      const tilesRow = focused.closest<HTMLElement>('[data-row-id="stats-tiles"]')
      if (tilesRow) {
        const tiles = Array.from(tilesRow.querySelectorAll<HTMLElement>('[data-nav-item]'))
        const idx = tiles.indexOf(focused)
        if (idx === -1) return
        const cols = getGridCols(tiles)
        e.preventDefault()
        if (e.key === 'ArrowRight') tiles[Math.min(idx + 1, tiles.length - 1)]?.focus()
        else if (e.key === 'ArrowLeft') tiles[Math.max(idx - 1, 0)]?.focus()
        else if (e.key === 'ArrowUp') {
          if (idx < cols) focusTopNavActive()
          else tiles[idx - cols]?.focus()
        } else if (e.key === 'ArrowDown') {
          if (idx >= tiles.length - cols) {
            const next = document.querySelector<HTMLElement>('[data-row-id="stats-expanded"] [data-nav-item]')
              ?? document.querySelector<HTMLElement>('[data-row-id="stats-day"] [data-nav-item]')
              ?? document.querySelector<HTMLElement>('[data-row-id="stats-actors"] [data-nav-item]')
            next?.focus()
          } else {
            tiles[Math.min(idx + cols, tiles.length - 1)]?.focus()
          }
        }
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [expanded])

  if (loading) {
    return <Layout><div className={styles.loading}>Загрузка…</div></Layout>
  }

  const s = stats ?? {
    movies_watched: 0, series_completed: 0, series_watching: 0,
    planned_movies: 0, planned_series: 0, stopped: 0, favorites: 0,
    episodes_watched: 0, watch_time_minutes: 0, remaining_minutes: 0,
    current_streak: 0, longest_streak: 0, favorite_weekday: -1,
    calendar: [] as DayActivity[],
    top_genres: [] as GenreCount[],
    top_actors: [] as ActorCount[],
  }

  const tiles: { label: string; value: number | string | { main: string; sub?: string }; kind?: ExpandedKind }[] = [
    { label: 'Фильмы', value: s.movies_watched, kind: 'movies' },
    { label: 'Сериалы', value: s.series_completed + s.series_watching, kind: 'series' },
    { label: 'Буду смотреть — фильмы', value: s.planned_movies, kind: 'planned_movies' },
    { label: 'Буду смотреть — сериалы', value: s.planned_series, kind: 'planned_series' },
    { label: 'Брошено', value: s.stopped, kind: 'stopped' },
    { label: 'В избранном', value: s.favorites, kind: 'favorites' },
    { label: 'Эпизодов просмотрено', value: s.episodes_watched },
    { label: 'Время у экрана', value: formatWatchTime(s.watch_time_minutes) },
    { label: 'Домотреть', value: formatWatchTime(s.remaining_minutes) },
    { label: 'Текущий стрик', value: `${s.current_streak} дн.` },
    { label: 'Самый длинный стрик', value: `${s.longest_streak} дн.` },
    { label: 'Любимый день недели', value: s.favorite_weekday >= 0 ? WEEKDAY_NAMES[s.favorite_weekday] : '—' },
  ]

  const expandedItems = expanded ? itemsCache[expanded] ?? null : null

  function tileBody(t: (typeof tiles)[number]) {
    const main = typeof t.value === 'object' ? t.value.main : typeof t.value === 'number' ? t.value.toLocaleString('ru') : t.value
    const sub = typeof t.value === 'object' ? t.value.sub : undefined
    return (
      <>
        <div className={styles.tileValue}>{main}</div>
        {sub && <div className={styles.tileSub}>{sub}</div>}
        <div className={styles.tileLabel}>{t.label}</div>
      </>
    )
  }

  return (
    <Layout>
      <div className={styles.page}>
        <h1 className={styles.title}>Статистика</h1>

        <div className={styles.tilesGrid} data-row-id="stats-tiles">
          {tiles.map(t => t.kind ? (
            <button
              key={t.label}
              type="button"
              className={`${styles.tile} ${styles.tileClickable}`}
              data-nav-item
              aria-expanded={expanded === t.kind}
              onClick={e => toggleExpanded(t.kind!, e.currentTarget)}
            >
              {tileBody(t)}
            </button>
          ) : (
            <div key={t.label} className={styles.tile}>
              {tileBody(t)}
            </div>
          ))}
        </div>

        {expanded && (
          <div className={styles.block} data-row-id="stats-expanded">
            <div className={styles.expandedHeader}>
              <h2 className={styles.blockTitle}>{EXPANDED_TITLES[expanded]}</h2>
              <button type="button" className={styles.closeBtn} onClick={() => { setExpanded(null); lastTileRef.current?.focus() }}>
                Свернуть
              </button>
            </div>
            {expanded === 'series' && (
              <p className={styles.blockSubtitle}>
                Завершено: {s.series_completed} · Смотрю сейчас: {s.series_watching}
                {seriesEpisodeTotals && seriesEpisodeTotals.total > 0 && (
                  // Scoped to these shows only (Смотрю + Завершено) — will
                  // legitimately differ from the "Эпизодов просмотрено"
                  // tile above, which also counts episodes from shows now
                  // Брошено/Не смотрю.
                  <> · Эпизодов в этих сериалах: {seriesEpisodeTotals.watched} из {seriesEpisodeTotals.total}</>
                )}
              </p>
            )}
            {expandedLoading ? (
              <p className={styles.emptyText}>Загрузка…</p>
            ) : !expandedItems || expandedItems.length === 0 ? (
              <p className={styles.emptyText}>Нет данных</p>
            ) : (
              <>
                <div className={styles.expandedGrid}>
                  {expandedItems.map(item => (
                    <StatsCard
                      key={cardIdOf(item)}
                      item={item}
                      kind={expanded}
                      onOpen={() => navigate(`/card/${cardIdOf(item)}`)}
                    />
                  ))}
                </div>
                {hasMoreCache[expanded] && <div ref={sentinelRef} className={styles.sentinel} />}
                {loadingMore && <p className={styles.emptyText}>Загрузка…</p>}
              </>
            )}
          </div>
        )}

        <div className={styles.calendarSection}>
          <ContributionCalendar data={s.calendar} onDayClick={openDay} />
        </div>

        {selectedDay && (
          <div className={styles.block} data-row-id="stats-day">
            <div className={styles.expandedHeader}>
              <h2 className={styles.blockTitle}>{formatDateRu(selectedDay)}</h2>
              <button type="button" className={styles.closeBtn} onClick={() => { setSelectedDay(null); setDayItems(null) }}>
                Свернуть
              </button>
            </div>
            {dayItemsLoading ? (
              <p className={styles.emptyText}>Загрузка…</p>
            ) : !dayItems || dayItems.length === 0 ? (
              <p className={styles.emptyText}>Нет данных</p>
            ) : (
              <div className={styles.expandedGrid}>
                {dayItems.map((item, i) => (
                  <DayItemCard
                    key={`${item.card_id}-${item.season ?? ''}-${item.episode ?? i}`}
                    item={item}
                    onOpen={() => navigate(`/card/${item.card_id}`)}
                  />
                ))}
              </div>
            )}
          </div>
        )}

        {s.top_genres.length > 0 && (
          <div className={styles.block}>
            <h2 className={styles.blockTitle}>Любимые жанры</h2>
            <p className={styles.blockSubtitle}>Фильмов и сериалов просмотрено в этом жанре</p>
            <div className={styles.genreList}>
              {s.top_genres.map(g => (
                <div key={g.name} className={styles.genreRow}>
                  <span className={styles.genreName}>{capitalizeFirst(g.name)}</span>
                  <div className={styles.genreBarTrack}>
                    <div className={styles.genreBarFill} style={{ width: `${(g.count / s.top_genres[0].count) * 100}%` }} />
                  </div>
                  <span className={styles.genreCount}>{g.count}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {s.top_actors.length > 0 && (
          <div className={styles.block}>
            <h2 className={styles.blockTitle}>Любимые актёры</h2>
            <div className={styles.actorsGrid} data-row-id="stats-actors">
              {s.top_actors.map(a => {
                const photo = a.profile_path ? tmdbUrl(a.profile_path, 'w185') : null
                return (
                  <Link key={a.person_id} to={`/actor/${a.person_id}`} className={styles.actorCard} data-nav-item>
                    {photo
                      ? <img className={styles.actorPhoto} src={photo} alt={a.name} loading="lazy" />
                      : <div className={styles.actorPhotoPlaceholder}>👤</div>}
                    <p className={styles.actorName}>{a.name}</p>
                    <p className={styles.actorCount}>Просмотрено: {a.count}</p>
                  </Link>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </Layout>
  )
}
