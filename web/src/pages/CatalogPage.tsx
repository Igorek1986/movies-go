import { useEffect, useState, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { posterUrl } from '@/utils/poster'
import styles from './CatalogPage.module.scss'

interface MediaItem {
  id: number
  media_type: string
  title: string
  poster_path: string | null
  vote_average: number
  release_date: string
  first_air_date: string
  release_quality: string
}

interface CatalogResponse {
  page: number
  total_pages: number
  total_results: number
  results: MediaItem[]
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

const CATEGORIES: { label: string; route: string }[] = [
  { label: 'Фильмы', route: 'movies' },
  { label: 'Фильмы (новые)', route: 'movies_new' },
  { label: 'Фильмы RU', route: 'movies_ru' },
  { label: 'Фильмы RU (новые)', route: 'movies_ru_new' },
  { label: '4K', route: 'movies_4k' },
  { label: '4K (новые)', route: 'movies_4k_new' },
  { label: 'Легенды', route: 'legends_id' },
  { label: 'Сериалы', route: 'tv_shows' },
  { label: 'Сериалы RU', route: 'tv_shows_ru' },
  { label: 'Мультфильмы', route: 'cartoon_movies' },
  { label: 'Мультсериалы', route: 'cartoon_series' },
  { label: 'Аниме', route: 'anime' },
]

const DEVICE_KEY = 'catalog_device'
const PROFILE_KEY = 'catalog_profile'

export default function CatalogPage() {
  const [category, setCategory] = useState('movies')
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [page, setPage] = useState(1)
  const [data, setData] = useState<CatalogResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [hideWatched, setHideWatched] = useState(false)

  const [devices, setDevices] = useState<Device[]>([])
  const [profiles, setProfiles] = useState<Profile[]>([])
  const [activeDevice, setActiveDevice] = useState<Device | null>(() => {
    try { return JSON.parse(localStorage.getItem(DEVICE_KEY) || 'null') } catch { return null }
  })
  const [activeProfile, setActiveProfile] = useState<Profile | null>(() => {
    try { return JSON.parse(localStorage.getItem(PROFILE_KEY) || 'null') } catch { return null }
  })

  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    async function load() {
      const devRes = await fetch('/api/devices')
      if (!devRes.ok) return
      const devList: Device[] = await devRes.json()
      setDevices(devList)

      // Restore or pick first device
      const savedDev = activeDevice
      const foundDev = savedDev ? devList.find(d => d.id === savedDev.id) : null
      const currentDev = foundDev ?? devList[0] ?? null
      if (currentDev && currentDev.id !== savedDev?.id) {
        selectDevice(currentDev, [])
      }

      // Load profiles for all devices
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

      // Restore or pick first profile for the current device
      const dev = foundDev ?? devList[0] ?? null
      if (!dev) return
      const devProfiles = allProfiles.filter(p => p.device_id === dev.id)
      const savedProf = activeProfile
      const foundProf = savedProf && savedProf.device_id === dev.id
        ? devProfiles.find(p => p.profile_id === savedProf.profile_id)
        : null
      if (!foundProf && devProfiles.length > 0) {
        selectProfile(devProfiles[0])
      }
    }
    load()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  function selectDevice(d: Device, currentProfiles: Profile[]) {
    setActiveDevice(d)
    localStorage.setItem(DEVICE_KEY, JSON.stringify(d))
    setPage(1)
    // Pick first profile of this device (use currentProfiles if profiles state not yet set)
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

  const deviceToken = activeDevice?.token ?? ''
  const visibleProfiles = profiles.filter(p => p.device_id === activeDevice?.id)

  const load = useCallback(() => {
    setLoading(true)
    const params = new URLSearchParams({ page: String(page), per_page: '24' })
    if (search) params.set('search', search)
    if (deviceToken && activeProfile) {
      params.set('token', deviceToken)
      params.set('profile_id', activeProfile.profile_id)
      if (hideWatched) params.set('hide_watched', '1')
    }
    fetch(`/${category}?${params}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => setData(d))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [category, page, search, deviceToken, activeProfile, hideWatched])

  useEffect(() => { load() }, [load])

  function handleCategoryChange(route: string) {
    setCategory(route)
    setPage(1)
    setSearch('')
    setSearchInput('')
  }

  function handleSearch(e: React.FormEvent) {
    e.preventDefault()
    setSearch(searchInput)
    setPage(1)
  }

  const totalPages = data?.total_pages ?? 1
  const year = (item: MediaItem) => (item.release_date || item.first_air_date || '').slice(0, 4)

  return (
    <Layout>
      <div className={styles.page}>

        {/* ── Device + profile selector ── */}
        {devices.length > 0 && (
          <div className={styles.selectorBar}>
            <div className={styles.selectorRow}>
              {/* Device tabs */}
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

              {/* Profile tabs for selected device */}
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

            <div className={styles.profileBarRight}>
              {activeProfile && (
                <button
                  className={`${styles.hideWatchedBtn} ${hideWatched ? styles.hideWatchedActive : ''}`}
                  onClick={() => { setHideWatched(h => !h); setPage(1) }}
                >
                  {hideWatched ? 'Скрыть просмотренное' : 'Показать все'}
                </button>
              )}
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
          </div>
        )}

        {/* ── Category tabs ── */}
        <div className={styles.categoryTabs}>
          {CATEGORIES.map(c => (
            <button
              key={c.route}
              className={`${styles.categoryTab} ${category === c.route ? styles.categoryTabActive : ''}`}
              onClick={() => handleCategoryChange(c.route)}
            >
              {c.label}
            </button>
          ))}
        </div>

        {/* ── Results meta ── */}
        {data && (
          <div className={styles.meta}>
            {search && <span className={styles.searchTag}>«{search}» · </span>}
            {data.total_results.toLocaleString()} позиций
          </div>
        )}

        {loading && <div className={styles.empty}>Загрузка…</div>}

        {!loading && data?.results.length === 0 && (
          <div className={styles.empty}>Ничего не найдено</div>
        )}

        {!loading && data && data.results.length > 0 && (
          <div className={styles.grid}>
            {data.results.map(item => {
              const url = posterUrl(item.poster_path)
              const cardId = `${item.id}_${item.media_type}`
              return (
                <Link key={cardId} to={`/card/${cardId}`} className={styles.card}>
                  {url ? (
                    <img className={styles.poster} src={url} alt={item.title} loading="lazy" />
                  ) : (
                    <div className={styles.posterPlaceholder}>Нет постера</div>
                  )}
                  {item.media_type === 'tv' && <span className={styles.typeBadge}>Сериал</span>}
                  <div className={styles.cardBody}>
                    <p className={styles.cardTitle}>{item.title}</p>
                    <div className={styles.cardMeta}>
                      <span>{year(item)}</span>
                      {item.vote_average > 0 && <span>★ {item.vote_average.toFixed(1)}</span>}
                    </div>
                    {item.release_quality && (
                      <span className={styles.quality}>{item.release_quality}</span>
                    )}
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
