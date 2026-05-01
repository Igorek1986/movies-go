import { useEffect, useState, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import styles from './HistoryPage.module.scss'

interface HistoryItem {
  card_id: string
  tmdb_id: number
  media_type: string
  title: string
  poster_path: string | null
  year: string
  last_watched: string
  max_percent: number
  is_complete: boolean
}

interface HistoryCounts {
  all: number
  movies: number
  tv: number
  in_progress: number
}

interface HistoryResponse {
  page: number
  total_pages: number
  total_results: number
  counts: HistoryCounts
  results: HistoryItem[]
}

interface Device {
  id: number
  name: string
  token: string
}

interface Profile {
  device_id: number
  profile_id: string
  name: string
}

const SORT_OPTIONS = [
  { value: 'watched',      label: 'По дате просмотра' },
  { value: 'release',      label: 'По дате выхода' },
  { value: 'progress_asc', label: 'По прогрессу ↑' },
  { value: 'progress_desc',label: 'По прогрессу ↓' },
]

const DEVICE_KEY  = 'history_device'
const PROFILE_KEY = 'history_profile'

export default function HistoryPage() {
  const [data, setData]     = useState<HistoryResponse | null>(null)
  const [page, setPage]     = useState(1)
  const [loading, setLoading] = useState(false)

  const [mediaType,  setMediaType]  = useState('')       // '', 'movie', 'tv'
  const [inProgress, setInProgress] = useState(false)
  const [sort,       setSort]       = useState('watched')
  const [search,     setSearch]     = useState('')
  const [searchInput,setSearchInput]= useState('')

  const [devices,  setDevices]  = useState<Device[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [activeDevice,  setActiveDevice]  = useState<Device | null>(() => {
    try { return JSON.parse(localStorage.getItem(DEVICE_KEY) || 'null') } catch { return null }
  })
  const [activeProfile, setActiveProfile] = useState<Profile | null>(() => {
    try { return JSON.parse(localStorage.getItem(PROFILE_KEY) || 'null') } catch { return null }
  })

  const searchRef = useRef<HTMLInputElement>(null)

  // Load devices + profiles
  useEffect(() => {
    async function load() {
      const devRes = await fetch('/api/devices')
      if (!devRes.ok) return
      const devList: Device[] = await devRes.json()
      setDevices(devList)

      const savedDev = activeDevice
      const foundDev = savedDev ? devList.find(d => d.id === savedDev.id) : null
      const currentDev = foundDev ?? devList[0] ?? null

      const allProfiles: Profile[] = []
      await Promise.all(devList.map(async d => {
        const pRes = await fetch(`/api/devices/${d.id}/profiles`)
        if (!pRes.ok) return
        const pd: { profiles: { profile_id: string; name: string }[] } = await pRes.json()
        for (const p of pd.profiles) {
          allProfiles.push({ device_id: d.id, profile_id: p.profile_id, name: p.name })
        }
      }))
      setProfiles(allProfiles)

      if (!currentDev) return
      if (!foundDev) selectDevice(currentDev, allProfiles)

      const devProfiles = allProfiles.filter(p => p.device_id === currentDev.id)
      const savedProf = activeProfile
      const foundProf = savedProf?.device_id === currentDev.id
        ? devProfiles.find(p => p.profile_id === savedProf.profile_id)
        : null
      if (!foundProf && devProfiles.length > 0) selectProfile(devProfiles[0])
    }
    load()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function selectDevice(d: Device, currentProfiles: Profile[]) {
    setActiveDevice(d)
    localStorage.setItem(DEVICE_KEY, JSON.stringify(d))
    setPage(1)
    const source = currentProfiles.length > 0 ? currentProfiles : profiles
    const devProfiles = source.filter(p => p.device_id === d.id)
    if (devProfiles.length > 0) {
      selectProfile(devProfiles[0])
    } else {
      setActiveProfile(null)
      localStorage.removeItem(PROFILE_KEY)
    }
  }

  function selectProfile(p: Profile) {
    setActiveProfile(p)
    localStorage.setItem(PROFILE_KEY, JSON.stringify(p))
    setPage(1)
  }

  function handleFilterTab(type: string) {
    if (type === 'in_progress') {
      setInProgress(true)
      setMediaType('')
    } else {
      setInProgress(false)
      setMediaType(type)
    }
    setPage(1)
  }

  function activeFilterKey() {
    if (inProgress) return 'in_progress'
    return mediaType || 'all'
  }

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    setSearch(searchInput)
    setPage(1)
  }

  const visibleProfiles = profiles.filter(p => p.device_id === activeDevice?.id)

  // Fetch history
  useEffect(() => {
    if (!activeDevice || !activeProfile) return
    setLoading(true)
    const params = new URLSearchParams({
      page:       String(page),
      per_page:   '24',
      device_id:  String(activeDevice.id),
      profile_id: activeProfile.profile_id,
      sort,
    })
    if (mediaType)  params.set('media_type',  mediaType)
    if (inProgress) params.set('in_progress', '1')
    if (search)     params.set('search',      search)

    fetch(`/api/web/history?${params}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => setData(d))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [activeDevice, activeProfile, page, mediaType, inProgress, sort, search])

  const counts = data?.counts
  const totalPages = data?.total_pages ?? 1

  const filterTabs = [
    { key: 'all',         label: 'Все',         count: counts?.all },
    { key: 'movie',       label: 'Фильмы',      count: counts?.movies },
    { key: 'tv',          label: 'Сериалы',      count: counts?.tv },
    { key: 'in_progress', label: 'В процессе',  count: counts?.in_progress },
  ]

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Device + profile selector ── */}
        {devices.length > 0 && (
          <div className={styles.selectorBar}>
            <div className={styles.selectorRow}>
              <div className={styles.deviceTabs}>
                {devices.map(d => (
                  <button
                    key={d.id}
                    className={`${styles.deviceTab} ${activeDevice?.id === d.id ? styles.deviceTabActive : ''}`}
                    onClick={() => selectDevice(d, profiles)}
                  >
                    {d.name}
                  </button>
                ))}
              </div>
              {visibleProfiles.length > 0 && (
                <div className={styles.profileTabs}>
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

            <form className={styles.searchForm} onSubmit={handleSearch}>
              <div className={styles.searchWrap}>
                <span className={styles.searchIcon}>🔍</span>
                <input
                  ref={searchRef}
                  className={styles.searchInput}
                  placeholder="Поиск…"
                  value={searchInput}
                  onChange={e => setSearchInput(e.target.value)}
                />
              </div>
            </form>
          </div>
        )}

        {/* ── Filter tabs + sort ── */}
        {data && (
          <div className={styles.controlsBar}>
            <div className={styles.filterTabs}>
              {filterTabs.map(t => (
                <button
                  key={t.key}
                  className={`${styles.filterTab} ${activeFilterKey() === t.key ? styles.filterTabActive : ''}`}
                  onClick={() => handleFilterTab(t.key === 'all' ? '' : t.key)}
                >
                  {t.label}{t.count !== undefined ? ` (${t.count})` : ''}
                </button>
              ))}
            </div>
            <select
              className={styles.sortSelect}
              value={sort}
              onChange={e => { setSort(e.target.value); setPage(1) }}
            >
              {SORT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && !activeProfile && (
          <div className={styles.empty}>Выберите устройство и профиль</div>
        )}

        {!loading && activeProfile && data?.results.length === 0 && (
          <div className={styles.empty}>История пуста</div>
        )}

        {!loading && data && data.results.length > 0 && (
          <div className={styles.grid}>
            {data.results.map(item => {
              const url = posterUrl(item.poster_path)
              return (
                <Link key={item.card_id} to={`/card/${item.card_id}`} className={styles.card}>
                  {url ? (
                    <img className={styles.poster} src={url} alt={item.title} loading="lazy" />
                  ) : (
                    <div className={styles.posterPlaceholder}>Нет постера</div>
                  )}
                  {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
                  <div className={styles.cardBody}>
                    <p className={styles.cardTitle}>{item.title}</p>
                    {item.max_percent > 0 && (
                      <div className={styles.progress}>
                        <div
                          className={styles.progressBar}
                          style={{ width: `${Math.min(item.max_percent, 100)}%` }}
                        />
                      </div>
                    )}
                    <div className={styles.cardMeta}>
                      <span>{item.year}</span>
                      {item.max_percent > 0 && (
                        <span className={item.is_complete ? styles.complete : ''}>
                          {Math.round(item.max_percent)}%
                        </span>
                      )}
                    </div>
                  </div>
                </Link>
              )
            })}
          </div>
        )}

        {totalPages > 1 && (
          <div className={styles.pagination}>
            <button className={styles.pageBtn} disabled={page <= 1} onClick={() => setPage(p => p - 1)}>← Назад</button>
            <button className={`${styles.pageBtn} ${styles.current}`}>{page} / {totalPages}</button>
            <button className={styles.pageBtn} disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>Вперёд →</button>
          </div>
        )}

      </div>
    </Layout>
  )
}
