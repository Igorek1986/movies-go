import { useEffect, useState, useRef, useLayoutEffect, useCallback, useMemo } from 'react'
import { useParams, Link, useLocation } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl, tmdbUrl } from '@/utils/poster'
import { useAuth } from '@/hooks/useAuth'
import styles from './CardDetailPage.module.scss'

// ── Types ─────────────────────────────────────────────────────────────────────

interface CardDetail {
  card_id: string; tmdb_id: number; media_type: string
  title: string; original_title: string; overview: string
  poster_path: string; backdrop_path: string
  release_date: string; first_air_date: string; last_air_date: string; year: string
  vote_average: number; vote_count: number
  runtime: number; episode_run_time: number
  original_language: string; adult: boolean; status: string
  number_of_seasons: number; number_of_episodes: number
  seasons: Season[] | null
  age_rating: number; certification_ru: string
  genres: Genre[]; best_video_quality: number
  torrent_date: string; rutor_category: string; imdb_id: string
  movie_item: string
}
interface Genre   { id: number; name: string }
interface Season  { season_number: number; name: string; episode_count: number; air_date: string }
interface CastMember { id: number; name: string; character: string; profile_path: string | null }
interface SimilarItem { card_id: string; tmdb_id: number; media_type: string; title: string; poster_path: string; year: string }
interface Device { id: number; name: string; token: string }
interface Profile { device_id: number; profile_id: string; name: string }
interface CardTimecode { item: string; percent: number; time: number; duration_sec: number | null; profile_id: string; special: boolean }
interface TimePickerCtx { initialSec: number; maxSec: number; item: string; profileId: string }
interface EpisodeData {
  season: number; episode: number; title: string | null
  hash: string; watched: boolean; special: boolean; user_special: boolean; catalog_special: boolean
  percent: number; duration_sec: number | null; future: boolean; air_date: string | null
}

// ── Media hash ────────────────────────────────────────────────────────────────

function mediaHash(s: string): string {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  }
  return String(Math.abs(h))
}

function episodeItem(season: number, ep: number, origTitle: string): string {
  const s = season > 10 ? `${season}:${ep}${origTitle}` : `${season}${ep}${origTitle}`
  return mediaHash(s)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function loadActiveDevice(): Device | null {
  try { const v = localStorage.getItem('active_device'); return v ? JSON.parse(v) : null } catch { return null }
}
function loadActiveProfile(): Profile | null {
  try { const v = localStorage.getItem('active_profile'); return v ? JSON.parse(v) : null } catch { return null }
}
function saveActiveDevice(d: Device | null) {
  try { d ? localStorage.setItem('active_device', JSON.stringify(d)) : localStorage.removeItem('active_device') } catch {}
}
function saveActiveProfile(p: Profile | null) {
  try { p ? localStorage.setItem('active_profile', JSON.stringify(p)) : localStorage.removeItem('active_profile') } catch {}
}

function fmtTime(sec: number): string {
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = Math.floor(sec % 60)
  if (h > 0) return `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}`
  return `${m}:${String(s).padStart(2,'0')}`
}

function qualityLabel(q: number) {
  if (q >= 300) return '4K'; if (q >= 200) return '1080p'; if (q >= 100) return '720p'; return 'SD'
}
function runtimeLabel(m: number) {
  if (!m) return ''; const h = Math.floor(m/60); const r = m%60
  if (!h) return `${r} мин`; return r ? `${h} ч ${r} мин` : `${h} ч`
}

// ── Drum Column ───────────────────────────────────────────────────────────────

const ITEM_H = 44

function DrumColumn({ count, value, onChange, label }: {
  count: number; value: number; onChange: (v: number) => void; label: string
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const skipSync  = useRef(false)
  const timer     = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  useLayoutEffect(() => {
    if (skipSync.current || !scrollRef.current) return
    scrollRef.current.scrollTop = value * ITEM_H
  }, [value])

  function handleScroll() {
    skipSync.current = true
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(() => {
      if (!scrollRef.current) return
      const idx = Math.round(scrollRef.current.scrollTop / ITEM_H)
      const v = Math.max(0, Math.min(count - 1, idx))
      scrollRef.current.scrollTop = v * ITEM_H
      skipSync.current = false
      onChange(v)
    }, 120)
  }

  function handleInput(raw: string) {
    const v = Math.max(0, Math.min(count - 1, parseInt(raw, 10) || 0))
    onChange(v)
    if (scrollRef.current) scrollRef.current.scrollTop = v * ITEM_H
  }

  return (
    <div className={styles.drumCol}>
      <div className={styles.drumOuter}>
        <div className={styles.drumBand} />
        <div className={styles.drumFadeTop} />
        <div className={styles.drumFadeBot} />
        <div className={styles.drumScroll} ref={scrollRef} onScroll={handleScroll}>
          <div className={styles.drumPad} />
          {Array.from({ length: count }, (_, i) => (
            <div key={i} className={`${styles.drumItem} ${i === value ? styles.drumItemActive : ''}`}>
              {String(i).padStart(2, '0')}
            </div>
          ))}
          <div className={styles.drumPad} />
        </div>
      </div>
      <input
        type="number" min={0} max={count - 1} value={value}
        onChange={e => handleInput(e.target.value)}
        className={styles.drumInput}
      />
      <span className={styles.drumLabel}>{label}</span>
    </div>
  )
}

// ── Time Picker ───────────────────────────────────────────────────────────────

function TimePicker({ ctx, onConfirm, onCancel }: {
  ctx: TimePickerCtx
  onConfirm: (ctx: TimePickerCtx, sec: number) => void
  onCancel: () => void
}) {
  // ── Percent-only mode when duration is unknown ────────────────────────────
  const [pct, setPct] = useState(ctx.initialSec)  // initialSec is percent (0-100) in this mode

  if (ctx.maxSec === 0) {
    return (
      <div className={styles.tpOverlay} onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
        <div className={styles.tpDialog}>
          <p className={styles.tpTitle}>Установить прогресс</p>
          <p className={styles.tpNote}>Длительность неизвестна</p>
          <div className={styles.pctRow}>
            <input
              type="number" min={0} max={100}
              value={pct}
              onChange={e => setPct(Math.min(100, Math.max(0, Math.round(+e.target.value))))}
              className={styles.pctInput}
            />
            <span className={styles.pctSign}>%</span>
          </div>
          <div className={styles.tpBtns}>
            <button className={styles.tpOk} onClick={() => onConfirm(ctx, pct)}>OK</button>
            <button className={styles.tpCancel} onClick={onCancel}>Отмена</button>
          </div>
        </div>
      </div>
    )
  }

  // ── Time drum picker ──────────────────────────────────────────────────────
  const showH  = ctx.maxSec >= 3600
  const capH   = showH ? Math.floor(ctx.maxSec / 3600) : 0

  const [h, setH] = useState(Math.floor(ctx.initialSec / 3600))
  const [m, setM] = useState(Math.floor((ctx.initialSec % 3600) / 60))
  const [s, setS] = useState(ctx.initialSec % 60)

  // Dynamic limits based on current h/m values
  const capM = (showH && h < capH) ? 59 : Math.floor((ctx.maxSec - h * 3600) / 60)
  const capS = (showH && h < capH) || m < capM ? 59 : ctx.maxSec - h * 3600 - m * 60

  function changeH(newH: number) {
    setH(newH)
    const newCapM = newH < capH ? 59 : Math.floor((ctx.maxSec - newH * 3600) / 60)
    const clampedM = Math.min(m, newCapM)
    if (clampedM !== m) setM(clampedM)
    const newCapS = (newH < capH || clampedM < newCapM)
      ? 59
      : ctx.maxSec - newH * 3600 - clampedM * 60
    if (s > newCapS) setS(newCapS)
  }

  function changeM(newM: number) {
    setM(newM)
    const newCapS = (showH && h < capH) || newM < Math.floor((ctx.maxSec - h * 3600) / 60)
      ? 59
      : ctx.maxSec - h * 3600 - newM * 60
    if (s > newCapS) setS(newCapS)
  }

  return (
    <div className={styles.tpOverlay} onClick={e => { if (e.target === e.currentTarget) onCancel() }}>
      <div className={styles.tpDialog}>
        <p className={styles.tpTitle}>Установить время</p>

        <div className={styles.drumRow}>
          {showH && (
            <>
              <DrumColumn count={capH + 1} value={h} onChange={changeH} label="ч" />
              <span className={styles.drumSep}>:</span>
            </>
          )}
          <DrumColumn count={capM + 1} value={m} onChange={changeM} label="мин" />
          <span className={styles.drumSep}>:</span>
          <DrumColumn count={capS + 1} value={s} onChange={setS} label="сек" />
        </div>

        <div className={styles.tpBtns}>
          <button className={styles.tpOk} onClick={() => onConfirm(ctx, h * 3600 + m * 60 + s)}>OK</button>
          <button className={styles.tpCancel} onClick={onCancel}>Отмена</button>
        </div>
      </div>
    </div>
  )
}

// ── Interactive bar ───────────────────────────────────────────────────────────

function InteractiveBar({ percent, onClick }: { percent: number; onClick: (pct: number) => void }) {
  const ref = useRef<HTMLDivElement>(null)
  function pct(e: React.MouseEvent) {
    if (!ref.current) return 0
    const r = ref.current.getBoundingClientRect()
    return Math.min(100, Math.max(0, (e.clientX - r.left) / r.width * 100))
  }
  return (
    <div ref={ref} className={styles.iBar} onClick={e => onClick(pct(e))}>
      <div className={styles.iBarFill} style={{ width: `${Math.min(percent, 100)}%` }} />
    </div>
  )
}

// ── TV Episode List ───────────────────────────────────────────────────────────

function TvEpisodeList({ card, tcMap, defaultProfileId, epDurSec, onPickTime, onMarkAllWatched, onMarkSpecial, onUnmarkSpecial, apiEpisodes, timecodesLoaded, numberOfSeasons }: {
  card: CardDetail
  tcMap: Record<string, CardTimecode>
  defaultProfileId: string
  epDurSec: number
  onPickTime: (ctx: TimePickerCtx) => void
  onMarkAllWatched: (items: Array<{ item: string; profileId: string }>) => void
  onMarkSpecial: (item: string, profileId: string) => void
  onUnmarkSpecial: (item: string, profileId: string) => void
  apiEpisodes: EpisodeData[] | null
  timecodesLoaded: boolean
  numberOfSeasons: number
}) {
  // ── Source: apiEpisodes or card.seasons ──────────────────────────────────────
  const seasonGroupsFromAPI = useMemo(() => {
    if (!apiEpisodes) return null
    const m = new Map<number, EpisodeData[]>()
    for (const ep of apiEpisodes) {
      if (!m.has(ep.season)) m.set(ep.season, [])
      m.get(ep.season)!.push(ep)
    }
    return Array.from(m.entries()).sort(([a], [b]) => a - b)
  }, [apiEpisodes])

  const seasonsFromCard = useMemo(
    () => (card.seasons || []).filter(s => s.episode_count > 0).sort((a, b) => a.season_number - b.season_number),
    [card.seasons]
  )

  const useAPI = seasonGroupsFromAPI !== null

  const allSeasonNumbers = useMemo(() => {
    if (useAPI) return seasonGroupsFromAPI!.map(([sn]) => sn)
    if (seasonsFromCard.length > 0) return seasonsFromCard.map(s => s.season_number)
    // Fallback: generate stubs from numberOfSeasons when no detailed data yet
    if (numberOfSeasons > 0) return Array.from({ length: numberOfSeasons }, (_, i) => i + 1)
    return []
  }, [useAPI, seasonGroupsFromAPI, seasonsFromCard, numberOfSeasons])

  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const autoExpandedRef = useRef(false)

  useEffect(() => {
    if (autoExpandedRef.current) return
    // Wait until both episode list and timecodes are loaded
    if (!useAPI || !seasonGroupsFromAPI || !timecodesLoaded) return

    autoExpandedRef.current = true
    for (const [sn, eps] of seasonGroupsFromAPI) {
      if (sn === 0) continue
      const regular = eps.filter(ep => !ep.catalog_special)
      if (regular.length === 0) continue
      const hasUnwatched = regular.some(ep => {
        if (ep.user_special) return false
        const tc = tcMap[ep.hash]
        return !tc || (tc.percent < 90 && !tc.special)
      })
      if (hasUnwatched) { setExpanded(new Set([sn])); return }
    }
    // all regular episodes watched — keep all collapsed
  }, [useAPI, seasonGroupsFromAPI, tcMap, timecodesLoaded])

  function toggle(n: number) {
    setExpanded(prev => { const s = new Set(prev); s.has(n) ? s.delete(n) : s.add(n); return s })
  }

  function epCode(sn: number, ep: number) {
    return `S${String(sn).padStart(2,'0')}E${String(ep).padStart(2,'0')}`
  }

  // ── Watched counts ───────────────────────────────────────────────────────────
  function watchedAndTotal(sn: number): [number, number] {
    if (useAPI) {
      const allEps = seasonGroupsFromAPI!.find(([n]) => n === sn)?.[1] ?? []
      const eps = allEps.filter(ep => !ep.catalog_special) // exclude catalog specials from count
      if (eps.length === 0) return [0, allEps.length === 0 ? 0 : -1] // all specials → show ?
      const watched = eps.filter(ep => {
        if (ep.user_special) return true
        const tc = tcMap[ep.hash]
        return tc != null && (tc.percent >= 90 || tc.special)
      }).length
      return [watched, eps.length]
    }
    const season = seasonsFromCard.find(s => s.season_number === sn)
    if (!season) return [0, -1] // -1 = unknown count (stub mode)
    let n = 0
    for (let ep = 1; ep <= season.episode_count; ep++) {
      const tc = tcMap[episodeItem(sn, ep, card.original_title)]
      if (tc && (tc.percent >= 90 || tc.special)) n++
    }
    return [n, season.episode_count]
  }

  // ── Render one season ────────────────────────────────────────────────────────
  function renderSeason(sn: number) {
    const open = expanded.has(sn)
    const [watched, total] = watchedAndTotal(sn)

    function markAll(e: React.MouseEvent) {
      e.stopPropagation()
      if (useAPI) {
        const items = (seasonGroupsFromAPI!.find(([n]) => n === sn)?.[1] ?? [])
          .filter(ep => !ep.future && !ep.catalog_special)
          .map(ep => ({ item: ep.hash, profileId: tcMap[ep.hash]?.profile_id ?? defaultProfileId }))
        onMarkAllWatched(items)
      } else {
        const items = Array.from({ length: total }, (_, i) => {
          const ep = i + 1
          const item = episodeItem(sn, ep, card.original_title)
          return { item, profileId: tcMap[item]?.profile_id ?? defaultProfileId }
        })
        onMarkAllWatched(items)
      }
    }

    const isStub = total === -1
    return (
      <div key={sn} className={styles.seasonBlock}>
        <div className={styles.seasonHeader} onClick={() => toggle(sn)}>
          <span className={styles.seasonArrow}>{open ? '▼' : '▶'}</span>
          <span className={styles.seasonName}>{sn === 0 ? 'Спецэпизоды' : `Сезон ${sn}`}</span>
          <span className={styles.seasonCount}>{isStub ? '?' : `${watched}/${total}`}</span>
          {!isStub && <button className={styles.markAllBtn} onClick={markAll}>✓ Все</button>}
        </div>
        {open && !isStub && (
          <div className={styles.epList}>
            {useAPI
              ? renderEpisodesFromAPI(sn)
              : renderEpisodesFromSeasons(sn, total)
            }
          </div>
        )}
      </div>
    )
  }

  // ── API-mode episode rows ────────────────────────────────────────────────────
  function renderEpisodesFromAPI(sn: number) {
    const eps = seasonGroupsFromAPI!.find(([n]) => n === sn)?.[1] ?? []
    return eps.map(ep => {
      const tc = tcMap[ep.hash]
      const durSec = ep.duration_sec ?? tc?.duration_sec ?? epDurSec
      const timeSec = tc?.time ?? 0
      const profileId = tc?.profile_id ?? defaultProfileId
      const pct = tc?.percent ?? ep.percent
      // catalog_special = real special from episodes DB (e.g. season 0 extras)
      // user_special = user manually marked via ★ button
      // Show "спец" only for catalog specials; user-watched (via MyShows sync) shows as green bar
      const isCatalogSpecial = ep.catalog_special
      const isUserMarked = ep.user_special
      const isWatched = pct >= 90 || isUserMarked
      return (
        <div key={ep.episode} className={`${styles.epRow} ${isCatalogSpecial ? styles.epRowSpecial : ''} ${ep.future ? styles.epRowFuture : ''}`}>
          <div className={styles.epTop}>
            <span className={styles.epCode}>{epCode(sn, ep.episode)}</span>
            {ep.title && <span className={styles.epTitle}>{ep.title}</span>}
          </div>
          <div className={styles.epBottom}>
            <InteractiveBar
              percent={isWatched ? 100 : pct}
              onClick={clickPct => {
                if (isCatalogSpecial) return
                const initSec = durSec > 0 ? Math.round(durSec * clickPct / 100) : 0
                onPickTime({ initialSec: initSec, maxSec: durSec, item: ep.hash, profileId })
              }}
            />
            <span className={`${styles.epTime} ${isCatalogSpecial ? styles.epTimeSpecial : ''}`}>
              {isCatalogSpecial ? 'спец' : isWatched ? '✓' : pct > 0 ? fmtTime(timeSec) : '—'}
              /{durSec > 0 && !isCatalogSpecial ? fmtTime(durSec) : '—'}
            </span>
            {isUserMarked ? (
              <button className={styles.epUnspecial} onClick={() => onUnmarkSpecial(ep.hash, profileId)} title="Убрать отметку просмотра">↩</button>
            ) : isCatalogSpecial ? (
              <span className={styles.epSpecialBadge} title="Спецэпизод">★</span>
            ) : (
              <button className={styles.epSpecial} onClick={() => onMarkSpecial(ep.hash, profileId)} title="Отметить как просмотренный">★</button>
            )}
          </div>
        </div>
      )
    })
  }

  // ── Seasons-mode episode rows (fallback) ──────────────────────────────────────
  function renderEpisodesFromSeasons(sn: number, total: number) {
    return Array.from({ length: total }, (_, i) => {
      const ep = i + 1
      const item = episodeItem(sn, ep, card.original_title)
      const tc = tcMap[item]
      const pct = tc?.percent ?? 0
      const isSpecial = tc?.special ?? false
      const durSec = tc?.duration_sec ?? epDurSec
      const timeSec = tc?.time ?? 0
      const profileId = tc?.profile_id ?? defaultProfileId
      return (
        <div key={ep} className={`${styles.epRow} ${isSpecial ? styles.epRowSpecial : ''}`}>
          <div className={styles.epTop}>
            <span className={styles.epCode}>{epCode(sn, ep)}</span>
          </div>
          <div className={styles.epBottom}>
            <InteractiveBar
              percent={isSpecial ? 100 : pct}
              onClick={clickPct => {
                if (isSpecial) return
                const initSec = durSec > 0 ? Math.round(durSec * clickPct / 100) : 0
                onPickTime({ initialSec: initSec, maxSec: durSec, item, profileId })
              }}
            />
            <span className={`${styles.epTime} ${isSpecial ? styles.epTimeSpecial : ''}`}>
              {isSpecial ? 'спец' : pct > 0 ? fmtTime(timeSec) : '—'}/{durSec > 0 && !isSpecial ? fmtTime(durSec) : '—'}
            </span>
            {isSpecial ? (
              <button className={styles.epUnspecial} onClick={() => onUnmarkSpecial(item, profileId)} title="Убрать отметку спецэпизода">↩</button>
            ) : (
              <button className={styles.epSpecial} onClick={() => onMarkSpecial(item, profileId)} title="Отметить как спецэпизод (пропустить)">★</button>
            )}
          </div>
        </div>
      )
    })
  }

  if (allSeasonNumbers.length === 0) return null

  return (
    <section className={styles.section}>
      <div className={styles.epSummary}>
        {allSeasonNumbers.map(sn => {
          const [w, t] = watchedAndTotal(sn)
          return (
            <span key={sn} className={styles.epSummaryChip}>
              {sn === 0 ? 'Сп' : `С${sn}`}: {t === -1 ? '?' : `${w}/${t}`}
            </span>
          )
        })}
      </div>
      {allSeasonNumbers.map(renderSeason)}
    </section>
  )
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function CardDetailPage() {
  const { cardId } = useParams<{ cardId: string }>()
  const location   = useLocation()
  const backUrl    = (location.state as { backUrl?: string } | null)?.backUrl ?? `/catalog#${cardId}`
  const { user }   = useAuth()

  const [devices,       setDevices]      = useState<Device[]>([])
  const [profiles,      setProfiles]     = useState<Profile[]>([])
  const [activeDevice,  setActiveDevice] = useState<Device | null>(() => loadActiveDevice())
  const [activeProfile, setActiveProfile]= useState<Profile | null>(() => loadActiveProfile())

  const [card,         setCard]        = useState<CardDetail | null>(null)
  const [cast,         setCast]        = useState<CastMember[]>([])
  const [similar,      setSimilar]     = useState<SimilarItem[]>([])
  const [loading,      setLoading]     = useState(true)
  const [timecodes,    setTimecodes]   = useState<CardTimecode[]>([])
  const [timecodesLoaded, setTimecodesLoaded] = useState(false)
  const [tpCtx,        setTpCtx]      = useState<TimePickerCtx | null>(null)
  const [apiEpisodes,  setApiEpisodes] = useState<EpisodeData[] | null>(null)
  const [refreshing,   setRefreshing]  = useState(false)
  const [refreshed,    setRefreshed]   = useState(false)

  const activeProfileId = activeProfile?.profile_id ?? ''

  // Filter timecodes to the active profile
  const profileTimecodes = useMemo(() => {
    if (!activeProfileId) return timecodes
    return timecodes.filter(tc => tc.profile_id === activeProfileId)
  }, [timecodes, activeProfileId])

  const tcMap = useMemo(() => {
    const m: Record<string, CardTimecode> = {}
    for (const tc of profileTimecodes) m[tc.item] = tc
    return m
  }, [profileTimecodes])

  const defaultProfileId = activeProfileId || timecodes[0]?.profile_id || ''
  const visibleProfiles = profiles.filter(p => p.device_id === activeDevice?.id)

  const loadTimecodes = useCallback((cid: string, devId: number) => {
    fetch(`/api/web/card-timecodes?device_id=${devId}&card_id=${encodeURIComponent(cid)}`)
      .then(r => r.ok ? r.json() : [])
      .then((rows: CardTimecode[]) => { setTimecodes(rows ?? []); setTimecodesLoaded(true) })
      .catch(() => {})
  }, [])

  // Load devices and profiles; restore saved selection
  useEffect(() => {
    async function load() {
      try {
        const res = await fetch('/api/devices')
        if (!res.ok) return
        const devList: Device[] = await res.json()
        setDevices(devList)
        if (!devList.length) return

        const allProfiles: Profile[] = []
        await Promise.all(devList.map(async d => {
          try {
            const pRes = await fetch(`/api/devices/${d.id}/profiles`)
            if (!pRes.ok) return
            const pd: { profiles: { profile_id: string; name: string }[] } = await pRes.json()
            for (const p of pd.profiles) allProfiles.push({ device_id: d.id, profile_id: p.profile_id, name: p.name })
          } catch {}
        }))
        setProfiles(allProfiles)

        // Restore saved device (validate it still exists)
        const savedDev = loadActiveDevice()
        const chosenDev = devList.find(d => d.id === savedDev?.id) ?? devList[0]
        setActiveDevice(chosenDev)

        // Restore saved profile for this device
        const savedProf = loadActiveProfile()
        const devProfiles = allProfiles.filter(p => p.device_id === chosenDev.id)
        const chosenProf = devProfiles.find(p => p.profile_id === savedProf?.profile_id) ?? devProfiles[0] ?? null
        setActiveProfile(chosenProf)
      } catch {}
    }
    load()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!cardId) return
    setLoading(true); setTimecodes([]); setApiEpisodes(null); setTimecodesLoaded(false)

    fetch(`/api/media-card/${cardId}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => { setCard(d); setLoading(false) })
      .catch(() => setLoading(false))

    fetch(`/api/media-card/${cardId}/credits`)
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d?.cast) setCast(d.cast) })
      .catch(() => {})

    fetch(`/api/media-card/${cardId}/similar`)
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d?.items) setSimilar(d.items) })
      .catch(() => {})
  }, [cardId])

  // Reload timecodes when active device changes
  useEffect(() => {
    if (!cardId || !activeDevice) return
    loadTimecodes(cardId, activeDevice.id)
  }, [cardId, activeDevice?.id, loadTimecodes])

  // Load episodes; if source is not myshows — trigger sync and retry up to 3 times
  useEffect(() => {
    if (!card || card.media_type !== 'tv' || !cardId) return
    const cid = cardId
    const dev = activeDevice
    let cancelled = false
    const qs = new URLSearchParams({ card_id: cid, include_specials: '1' })
    if (dev) qs.set('device_id', String(dev.id))
    if (defaultProfileId) qs.set('profile_id', defaultProfileId)

    let retries = 0
    async function load() {
      if (cancelled) return
      try {
        const r = await fetch(`/api/episodes?${qs}`)
        if (!r.ok || cancelled) return
        const d = await r.json()
        if (d?.episodes?.length) setApiEpisodes(d.episodes)
        if (d?.source !== 'myshows' && retries < 3) {
          if (retries === 0 && dev && dev.token) {
            fetch(`/api/refresh-card-episodes?card_id=${encodeURIComponent(cid)}&token=${encodeURIComponent(dev.token)}`)
              .catch(() => {})
          }
          retries++
          setTimeout(load, 4000)
        }
      } catch {}
    }
    load()
    return () => { cancelled = true }
  }, [card?.card_id]) // eslint-disable-line react-hooks/exhaustive-deps

  function selectDevice(d: Device) {
    setActiveDevice(d)
    saveActiveDevice(d)
    const devProfiles = profiles.filter(p => p.device_id === d.id)
    const first = devProfiles[0] ?? null
    setActiveProfile(first)
    saveActiveProfile(first)
  }

  function selectProfile(p: Profile) {
    setActiveProfile(p)
    saveActiveProfile(p)
  }

  async function saveTimecodeForItem(ctx: TimePickerCtx, sec: number) {
    if (!activeDevice || !cardId) return
    const percent = ctx.maxSec > 0 ? Math.min(100, sec / ctx.maxSec * 100) : sec
    await fetch('/api/web/set-timecode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: activeDevice.id, card_id: cardId, item: ctx.item, percent, profile_id: ctx.profileId }),
    })
    loadTimecodes(cardId, activeDevice.id)
  }

  async function markAllWatched(items: Array<{ item: string; profileId: string }>) {
    if (!activeDevice || !cardId) return
    await Promise.all(items.map(({ item, profileId }) =>
      fetch('/api/web/set-timecode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ device_id: activeDevice.id, card_id: cardId, item, percent: 100, profile_id: profileId }),
      })
    ))
    loadTimecodes(cardId, activeDevice.id)
  }

  async function markSpecial(item: string, profileId: string) {
    if (!activeDevice || !cardId) return
    await fetch('/api/web/mark-special', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: activeDevice.id, card_id: cardId, item, profile_id: profileId }),
    })
    loadTimecodes(cardId, activeDevice.id)
  }

  async function unmarkSpecial(item: string, profileId: string) {
    if (!activeDevice || !cardId) return
    await fetch('/api/web/unmark-special', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: activeDevice.id, card_id: cardId, item, profile_id: profileId }),
    })
    loadTimecodes(cardId, activeDevice.id)
  }

  async function deleteAllTimecodes() {
    if (!activeDevice || !cardId || !confirm('Удалить историю просмотра?')) return
    const qs = new URLSearchParams({ device_id: String(activeDevice.id), card_id: cardId })
    await fetch(`/api/web/card-timecodes?${qs}`, { method: 'DELETE' })
    setTimecodes([])
  }

  async function refreshFromTMDB() {
    if (!cardId || refreshing) return
    setRefreshing(true); setRefreshed(false)
    try {
      const r = await fetch(`/api/admin/refresh-card/${cardId}`, { method: 'POST' })
      if (r.ok) {
        const updated = await fetch(`/api/media-card/${cardId}`).then(r => r.ok ? r.json() : null)
        if (updated) setCard(updated)
        setRefreshed(true)
        setTimeout(() => setRefreshed(false), 3000)
      }
    } finally {
      setRefreshing(false)
    }
  }

  function onMovieBarClick(clickPct: number, card: CardDetail) {
    if (!activeDevice) return
    const item = bestTc?.item ?? card.movie_item
    if (!item) return
    const dur = bestTc?.duration_sec ?? (card.runtime ? card.runtime * 60 : 0)
    // When dur=0 (unknown), pass clickPct directly as initialSec (treated as percent in picker)
    const initSec = dur > 0 ? Math.round(dur * clickPct / 100) : Math.round(clickPct)
    setTpCtx({ initialSec: initSec, maxSec: dur, item, profileId: bestTc?.profile_id ?? defaultProfileId })
  }

  if (loading) return <Layout><div className={styles.loading}>Загрузка…</div></Layout>

  if (!card) return (
    <Layout>
      <Link to={backUrl} className={styles.floatBack}>← Назад</Link>
      <div className={styles.notFound}>
        <p>Карточка не найдена</p>
      </div>
    </Layout>
  )

  const backdropSrc  = tmdbUrl(card.backdrop_path, 'w1280')
  const posterImgUrl = tmdbUrl(card.poster_path, 'w342') || posterUrl(card.poster_path)
  const isTV         = card.media_type === 'tv'
  const displayDate  = isTV ? card.first_air_date : card.release_date

  const tags: string[] = []
  if (card.year)              tags.push(card.year)
  if (card.certification_ru)  tags.push(card.certification_ru + '+')
  if (!isTV && card.runtime)  tags.push(runtimeLabel(card.runtime))
  if (isTV && card.episode_run_time) tags.push(runtimeLabel(card.episode_run_time) + '/эп')
  if (isTV && card.number_of_seasons) tags.push(`${card.number_of_seasons} сез.`)
  if (card.original_language && card.original_language !== 'ru') tags.push(card.original_language.toUpperCase())
  if (card.best_video_quality) tags.push(qualityLabel(card.best_video_quality))

  const bestTc = profileTimecodes.reduce<CardTimecode | null>(
    (b, tc) => (!b || tc.percent > b.percent) ? tc : b, null,
  )
  const moviePct = bestTc?.percent ?? 0
  const movieDur = bestTc?.duration_sec ?? (card.runtime ? card.runtime * 60 : 0)
  const showMovieProgress = !isTV

  // Overall TV progress denominator: prefer episodes table (non-special, aired), fall back to card.seasons
  const tvTotalEps = isTV ? (() => {
    if (apiEpisodes && apiEpisodes.length > 0) {
      const n = apiEpisodes.filter(ep => !ep.future && !ep.catalog_special).length
      if (n > 0) return n
    }
    return (card.seasons ?? []).filter(s => s.season_number > 0).reduce((s, ss) => s + ss.episode_count, 0)
  })() : 0
  const tvWatchedEps = isTV
    ? profileTimecodes.filter(tc => tc.percent >= 90 || tc.special).length
    : 0
  const tvProgress = tvTotalEps > 0 ? Math.min(100, tvWatchedEps * 100 / tvTotalEps) : 0

  const epDurSec = (card.episode_run_time || 0) * 60

  return (
    <Layout>
      <Link to={backUrl} className={styles.floatBack}>← Назад</Link>

      <div className={styles.page}>

        {/* ── Backdrop ── */}
        {backdropSrc && (
          <div className={styles.backdrop}>
            <img src={backdropSrc} alt="" aria-hidden />
            <div className={styles.backdropOverlay} />
          </div>
        )}

        {/* ── Hero ── */}
        <div className={`${styles.hero}${!backdropSrc ? ' ' + styles.heroNoBg : ''}`}>
          <div className={styles.posterWrap}>
            {posterImgUrl
              ? <img className={styles.poster} src={posterImgUrl} alt={card.title} />
              : <div className={styles.posterPlaceholder}>Нет постера</div>
            }
          </div>

          <div className={styles.heroInfo}>
            <h1 className={styles.title}>{card.title}</h1>
            {card.original_title && card.original_title !== card.title && (
              <p className={styles.origTitle}>{card.original_title}</p>
            )}

            <div className={styles.tags}>
              {isTV && <span className={styles.tagType}>Сериал</span>}
              {tags.map((t, i) => <span key={i} className={styles.tag}>{t}</span>)}
              {card.vote_average > 0 && <span className={styles.tagRating}>★ {card.vote_average.toFixed(1)}</span>}
            </div>

            {card.genres?.length > 0 && (
              <div className={styles.genres}>
                {card.genres.map(g => <span key={g.id} className={styles.genre}>{g.name}</span>)}
              </div>
            )}

            {/* ── Device / profile switcher ── */}
            {(devices.length > 1 || visibleProfiles.length > 0) && (
              <div className={styles.deviceSwitcher}>
                {devices.length > 1 && (
                  <div className={styles.deviceTabGroup}>
                    {devices.map(d => (
                      <button
                        key={d.id}
                        className={`${styles.deviceTab} ${activeDevice?.id === d.id ? styles.deviceTabActive : ''}`}
                        onClick={() => selectDevice(d)}
                      >
                        {d.name}
                      </button>
                    ))}
                  </div>
                )}
                {visibleProfiles.length > 0 && (
                  <div className={styles.profileTabGroup}>
                    {visibleProfiles.map(p => (
                      <button
                        key={p.profile_id}
                        className={`${styles.profileTab} ${activeProfile?.profile_id === p.profile_id ? styles.profileTabActive : ''}`}
                        onClick={() => selectProfile(p)}
                      >
                        {p.name}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* ── Movie progress ── */}
            {showMovieProgress && (
              <div className={styles.progressWrap}>
                <div className={styles.progressTop}>
                  <span className={`${styles.progressLabel} ${moviePct >= 90 ? styles.progressComplete : ''}`}>
                    {moviePct >= 90
                      ? 'Просмотрено'
                      : `Просмотрено ${Math.round(moviePct)}%`}
                    {movieDur > 0 && ` · ${fmtTime(Math.round(movieDur * moviePct / 100))} / ${fmtTime(movieDur)}`}
                  </span>
                  {moviePct > 0 && (
                    <button className={styles.deleteBtn} onClick={deleteAllTimecodes}>✕</button>
                  )}
                </div>
                <div
                  className={styles.movieBar}
                  onClick={e => {
                    const r = e.currentTarget.getBoundingClientRect()
                    onMovieBarClick(Math.min(100, Math.max(0, (e.clientX - r.left) / r.width * 100)), card)
                  }}
                >
                  <div className={styles.movieBarFill} style={{ width: `${Math.min(moviePct, 100)}%` }} />
                </div>
              </div>
            )}

            {/* ── TV overall progress ── */}
            {isTV && tvWatchedEps > 0 && (
              <div className={styles.progressWrap}>
                <div className={styles.progressTop}>
                  <span className={`${styles.progressLabel} ${tvProgress >= 100 ? styles.progressComplete : ''}`}>
                    {tvTotalEps > 0
                      ? `${tvWatchedEps} / ${tvTotalEps} серий`
                      : `${tvWatchedEps} серий просмотрено`}
                  </span>
                </div>
                <div className={styles.tvBar}>
                  <div className={styles.tvBarFill} style={{ width: `${tvProgress}%` }} />
                </div>
              </div>
            )}

            {card.overview && <p className={styles.overview}>{card.overview}</p>}

            {isTV && card.status && (
              <p className={styles.statusLine}>
                <span className={styles.statusLabel}>Статус:</span> {card.status}
              </p>
            )}
            {displayDate && (
              <p className={styles.statusLine}>
                <span className={styles.statusLabel}>{isTV ? 'Первый выход:' : 'Дата выхода:'}</span> {displayDate}
              </p>
            )}

            {user?.is_admin && (
              <button
                className={styles.refreshTmdbBtn}
                onClick={refreshFromTMDB}
                disabled={refreshing}
              >
                {refreshing ? 'Обновление…' : refreshed ? '✓ Обновлено' : '↻ Обновить из TMDB'}
              </button>
            )}
          </div>
        </div>

        {/* ── Cast ── */}
        {cast.length > 0 && (
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>В ролях</h2>
            <div className={styles.castRow}>
              {cast.map(c => {
                const photo = c.profile_path ? tmdbUrl(c.profile_path, 'w185') : null
                return (
                  <Link
                    key={c.id}
                    to={`/actor/${c.id}`}
                    className={styles.castCard}
                  >
                    {photo
                      ? <img className={styles.castPhoto} src={photo} alt={c.name} loading="lazy" />
                      : <div className={styles.castPhotoPlaceholder}>👤</div>
                    }
                    <p className={styles.castName}>{c.name}</p>
                    <p className={styles.castChar}>{c.character}</p>
                  </Link>
                )
              })}
            </div>
          </section>
        )}

        {/* ── TV Episode List ── */}
        {isTV && (!!apiEpisodes || (card.seasons && card.seasons.length > 0) || card.number_of_seasons > 0) && (
          <TvEpisodeList
            card={card}
            tcMap={tcMap}
            defaultProfileId={defaultProfileId}
            epDurSec={epDurSec}
            onPickTime={ctx => setTpCtx(ctx)}
            onMarkAllWatched={markAllWatched}
            onMarkSpecial={markSpecial}
            onUnmarkSpecial={unmarkSpecial}
            apiEpisodes={apiEpisodes}
            timecodesLoaded={timecodesLoaded}
            numberOfSeasons={card.number_of_seasons}
          />
        )}

        {/* ── Similar ── */}
        {similar.length > 0 && (
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Похожее</h2>
            <div className={styles.similarGrid}>
              {similar.map(item => {
                const url = tmdbUrl(item.poster_path, 'w185') || posterUrl(item.poster_path)
                return (
                  <Link key={item.card_id} to={`/card/${item.card_id}`} className={styles.similarCard}>
                    {url
                      ? <img className={styles.similarPoster} src={url} alt={item.title} loading="lazy" />
                      : <div className={styles.similarPosterPlaceholder}>Нет постера</div>
                    }
                    <p className={styles.similarTitle}>{item.title}</p>
                    <p className={styles.similarYear}>{item.year}</p>
                  </Link>
                )
              })}
            </div>
          </section>
        )}

      </div>

      {/* ── Time Picker ── */}
      {tpCtx && (
        <TimePicker
          ctx={tpCtx}
          onConfirm={async (ctx, sec) => { setTpCtx(null); await saveTimecodeForItem(ctx, sec) }}
          onCancel={() => setTpCtx(null)}
        />
      )}

    </Layout>
  )
}
