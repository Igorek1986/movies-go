import { useEffect, useState, useRef, useLayoutEffect, useCallback, useMemo } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl, tmdbUrl } from '@/utils/poster'
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
interface CardTimecode { item: string; percent: number; time: number; duration_sec: number | null; profile_id: string; special: boolean }
interface TimePickerCtx { initialSec: number; maxSec: number; item: string; profileId: string }
interface EpisodeData {
  season: number; episode: number; title: string | null
  hash: string; watched: boolean; special: boolean; user_special: boolean
  percent: number; duration_sec: number | null; future: boolean; air_date: string | null
}

// ── Lampa hash ────────────────────────────────────────────────────────────────

function lampaHash(s: string): string {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0
  }
  return String(Math.abs(h))
}

function episodeItem(season: number, ep: number, origTitle: string): string {
  const s = season > 10 ? `${season}:${ep}${origTitle}` : `${season}${ep}${origTitle}`
  return lampaHash(s)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function loadActiveDevice(): { id: number; name: string; token: string } | null {
  for (const key of ['catalog_device', 'history_device']) {
    try { const v = localStorage.getItem(key); if (v) return JSON.parse(v) } catch {}
  }
  return null
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

function TvEpisodeList({ card, tcMap, defaultProfileId, epDurSec, onPickTime, onMarkAllWatched, onMarkSpecial, onUnmarkSpecial, apiEpisodes, numberOfSeasons }: {
  card: CardDetail
  tcMap: Record<string, CardTimecode>
  defaultProfileId: string
  epDurSec: number
  onPickTime: (ctx: TimePickerCtx) => void
  onMarkAllWatched: (items: Array<{ item: string; profileId: string }>) => void
  onMarkSpecial: (item: string, profileId: string) => void
  onUnmarkSpecial: (item: string, profileId: string) => void
  apiEpisodes: EpisodeData[] | null
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

  const [expanded, setExpanded] = useState<Set<number>>(() => {
    const first = allSeasonNumbers.find(n => n > 0) ?? allSeasonNumbers[0]
    return new Set(first != null ? [first] : [])
  })

  function toggle(n: number) {
    setExpanded(prev => { const s = new Set(prev); s.has(n) ? s.delete(n) : s.add(n); return s })
  }

  function epCode(sn: number, ep: number) {
    return `S${String(sn).padStart(2,'0')}E${String(ep).padStart(2,'0')}`
  }

  // ── Watched counts ───────────────────────────────────────────────────────────
  function watchedAndTotal(sn: number): [number, number] {
    if (useAPI) {
      const eps = seasonGroupsFromAPI!.find(([n]) => n === sn)?.[1] ?? []
      const watched = eps.filter(e => e.watched || e.special).length
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
          .filter(ep => !ep.future)
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
      const isUserSpecial = ep.user_special
      const isAnySpecial  = ep.special
      return (
        <div key={ep.episode} className={`${styles.epRow} ${isAnySpecial ? styles.epRowSpecial : ''} ${ep.future ? styles.epRowFuture : ''}`}>
          <span className={styles.epCode}>
            {epCode(sn, ep.episode)}
            {ep.title && <span className={styles.epTitle}>{ep.title}</span>}
          </span>
          <InteractiveBar
            percent={isAnySpecial ? 100 : ep.percent}
            onClick={clickPct => {
              if (isAnySpecial) return
              const initSec = durSec > 0 ? Math.round(durSec * clickPct / 100) : 0
              onPickTime({ initialSec: initSec, maxSec: durSec, item: ep.hash, profileId })
            }}
          />
          <span className={`${styles.epTime} ${isAnySpecial ? styles.epTimeSpecial : ''}`}>
            {isAnySpecial ? 'спец' : ep.percent > 0 ? fmtTime(timeSec) : '—'}
            /{durSec > 0 && !isAnySpecial ? fmtTime(durSec) : '—'}
          </span>
          {isUserSpecial ? (
            <button className={styles.epUnspecial} onClick={() => onUnmarkSpecial(ep.hash, profileId)} title="Убрать отметку спецэпизода">↩</button>
          ) : isAnySpecial ? (
            <span className={styles.epSpecialBadge} title="Спецэпизод (MyShows)">★</span>
          ) : (
            <button className={styles.epSpecial} onClick={() => onMarkSpecial(ep.hash, profileId)} title="Отметить как спецэпизод (пропустить)">★</button>
          )}
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
          <span className={styles.epCode}>{epCode(sn, ep)}</span>
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
  const navigate   = useNavigate()
  const dev        = useMemo(() => loadActiveDevice(), [])

  const [card,         setCard]        = useState<CardDetail | null>(null)
  const [cast,         setCast]        = useState<CastMember[]>([])
  const [similar,      setSimilar]     = useState<SimilarItem[]>([])
  const [loading,      setLoading]     = useState(true)
  const [timecodes,    setTimecodes]   = useState<CardTimecode[]>([])
  const [tpCtx,        setTpCtx]      = useState<TimePickerCtx | null>(null)
  const [apiEpisodes,  setApiEpisodes] = useState<EpisodeData[] | null>(null)

  const tcMap = useMemo(() => {
    const m: Record<string, CardTimecode> = {}
    for (const tc of timecodes) m[tc.item] = tc
    return m
  }, [timecodes])

  const defaultProfileId = timecodes[0]?.profile_id ?? ''

  const loadTimecodes = useCallback((cid: string, devId: number) => {
    fetch(`/api/web/card-timecodes?device_id=${devId}&card_id=${encodeURIComponent(cid)}`)
      .then(r => r.ok ? r.json() : [])
      .then((rows: CardTimecode[]) => setTimecodes(rows ?? []))
      .catch(() => {})
  }, [])

  useEffect(() => {
    if (!cardId) return
    setLoading(true); setTimecodes([]); setApiEpisodes(null)

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

    if (dev) loadTimecodes(cardId, dev.id)
  }, [cardId]) // eslint-disable-line react-hooks/exhaustive-deps

  // Load episodes from MyShows/TMDB API once the card is known to be a TV show
  useEffect(() => {
    if (!card || card.media_type !== 'tv' || !cardId) return
    const qs = new URLSearchParams({ card_id: cardId, include_specials: '1' })
    if (dev) qs.set('device_id', String(dev.id))
    if (defaultProfileId) qs.set('profile_id', defaultProfileId)
    fetch(`/api/episodes?${qs}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => { if (d?.episodes?.length) setApiEpisodes(d.episodes) })
      .catch(() => {})
  }, [card?.card_id]) // eslint-disable-line react-hooks/exhaustive-deps

  async function saveTimecodeForItem(ctx: TimePickerCtx, sec: number) {
    if (!dev || !cardId) return
    const percent = ctx.maxSec > 0 ? Math.min(100, sec / ctx.maxSec * 100) : 0
    await fetch('/api/web/set-timecode', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: dev.id, card_id: cardId, item: ctx.item, percent, profile_id: ctx.profileId }),
    })
    loadTimecodes(cardId, dev.id)
  }

  async function markAllWatched(items: Array<{ item: string; profileId: string }>) {
    if (!dev || !cardId) return
    await Promise.all(items.map(({ item, profileId }) =>
      fetch('/api/web/set-timecode', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ device_id: dev.id, card_id: cardId, item, percent: 100, profile_id: profileId }),
      })
    ))
    loadTimecodes(cardId, dev.id)
  }

  async function markSpecial(item: string, profileId: string) {
    if (!dev || !cardId) return
    await fetch('/api/web/mark-special', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: dev.id, card_id: cardId, item, profile_id: profileId }),
    })
    loadTimecodes(cardId, dev.id)
  }

  async function unmarkSpecial(item: string, profileId: string) {
    if (!dev || !cardId) return
    await fetch('/api/web/unmark-special', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ device_id: dev.id, card_id: cardId, item, profile_id: profileId }),
    })
    loadTimecodes(cardId, dev.id)
  }

  async function deleteAllTimecodes() {
    if (!dev || !cardId || !confirm('Удалить историю просмотра?')) return
    const qs = new URLSearchParams({ device_id: String(dev.id), card_id: cardId })
    await fetch(`/api/web/card-timecodes?${qs}`, { method: 'DELETE' })
    setTimecodes([])
  }

  function onMovieBarClick(clickPct: number, card: CardDetail) {
    const item = bestTc?.item ?? card.movie_item
    if (!item) return
    const dur = bestTc?.duration_sec ?? (card.runtime ? card.runtime * 60 : 0)
    const initSec = dur > 0 ? Math.round(dur * clickPct / 100) : 0
    setTpCtx({ initialSec: initSec, maxSec: dur, item, profileId: bestTc?.profile_id ?? defaultProfileId })
  }

  if (loading) return <Layout><div className={styles.loading}>Загрузка…</div></Layout>

  if (!card) return (
    <Layout>
      <div className={styles.notFound}>
        <p>Карточка не найдена</p>
        <button className={styles.backBtn} onClick={() => navigate(-1)}>← Назад</button>
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

  const bestTc = timecodes.reduce<CardTimecode | null>(
    (b, tc) => (!b || tc.percent > b.percent) ? tc : b, null,
  )
  const moviePct = bestTc?.percent ?? 0
  const movieDur = bestTc?.duration_sec ?? (card.runtime ? card.runtime * 60 : 0)
  const showMovieProgress = !isTV && (!!card.movie_item || !!bestTc)

  const epDurSec = (card.episode_run_time || 0) * 60

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Backdrop ── */}
        {backdropSrc && (
          <div className={styles.backdrop}>
            <img src={backdropSrc} alt="" aria-hidden />
            <div className={styles.backdropOverlay} />
          </div>
        )}

        {/* ── Hero ── */}
        <div className={styles.hero}>
          <div className={styles.posterWrap}>
            {posterImgUrl
              ? <img className={styles.poster} src={posterImgUrl} alt={card.title} />
              : <div className={styles.posterPlaceholder}>Нет постера</div>
            }
          </div>

          <div className={styles.heroInfo}>
            <button className={styles.backBtn} onClick={() => navigate(-1)}>← Назад</button>
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

            {/* ── Movie progress ── */}
            {showMovieProgress && dev && (
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
                <InteractiveBar percent={moviePct} onClick={pct => onMovieBarClick(pct, card)} />
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
                  <a
                    key={c.id}
                    href={`https://www.themoviedb.org/person/${c.id}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.castCard}
                  >
                    {photo
                      ? <img className={styles.castPhoto} src={photo} alt={c.name} loading="lazy" />
                      : <div className={styles.castPhotoPlaceholder}>👤</div>
                    }
                    <p className={styles.castName}>{c.name}</p>
                    <p className={styles.castChar}>{c.character}</p>
                  </a>
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
