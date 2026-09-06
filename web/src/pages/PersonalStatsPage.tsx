import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import ContributionCalendar from '@/components/ContributionCalendar'
import { useActiveProfile } from '@/contexts/ActiveProfileContext'
import { tmdbUrl } from '@/utils/poster'
import styles from './PersonalStatsPage.module.scss'

interface DayActivity {
  date: string
  count: number
}

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
  current_streak: number
  longest_streak: number
  favorite_weekday: number // 0=Mon..6=Sun, -1 = no data
  calendar: DayActivity[]
  top_genres: GenreCount[]
  top_actors: ActorCount[]
}

const WEEKDAY_NAMES = ['Понедельник', 'Вторник', 'Среда', 'Четверг', 'Пятница', 'Суббота', 'Воскресенье']

function formatHours(minutes: number): string {
  const hours = Math.round(minutes / 60)
  return `${hours.toLocaleString('ru')} ч`
}

export default function PersonalStatsPage() {
  const { activeDevice, activeProfile, loaded } = useActiveProfile()
  const [stats, setStats] = useState<ProfileStats | null>(null)
  const [loading, setLoading] = useState(true)

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
  }, [loaded, token, profileId])

  if (loading) {
    return <Layout><div className={styles.loading}>Загрузка…</div></Layout>
  }

  const s = stats ?? {
    movies_watched: 0, series_completed: 0, series_watching: 0,
    planned_movies: 0, planned_series: 0, stopped: 0, favorites: 0,
    episodes_watched: 0, watch_time_minutes: 0,
    current_streak: 0, longest_streak: 0, favorite_weekday: -1,
    calendar: [] as DayActivity[],
    top_genres: [] as GenreCount[],
    top_actors: [] as ActorCount[],
  }

  const tiles = [
    { label: 'Фильмов просмотрено', value: s.movies_watched },
    { label: 'Сериалов завершено', value: s.series_completed },
    { label: 'Сериалов смотрю сейчас', value: s.series_watching },
    { label: 'Буду смотреть — фильмы', value: s.planned_movies },
    { label: 'Буду смотреть — сериалы', value: s.planned_series },
    { label: 'Брошено', value: s.stopped },
    { label: 'В избранном', value: s.favorites },
    { label: 'Эпизодов просмотрено', value: s.episodes_watched },
    { label: 'Время у экрана', value: formatHours(s.watch_time_minutes) },
    { label: 'Текущий стрик', value: `${s.current_streak} дн.` },
    { label: 'Самый длинный стрик', value: `${s.longest_streak} дн.` },
    { label: 'Любимый день недели', value: s.favorite_weekday >= 0 ? WEEKDAY_NAMES[s.favorite_weekday] : '—' },
  ]

  return (
    <Layout>
      <div className={styles.page}>
        <h1 className={styles.title}>Статистика</h1>

        <div className={styles.tilesGrid}>
          {tiles.map(t => (
            <div key={t.label} className={styles.tile}>
              <div className={styles.tileValue}>{typeof t.value === 'number' ? t.value.toLocaleString('ru') : t.value}</div>
              <div className={styles.tileLabel}>{t.label}</div>
            </div>
          ))}
        </div>

        <div className={styles.calendarSection}>
          <ContributionCalendar data={s.calendar} />
        </div>

        {s.top_genres.length > 0 && (
          <div className={styles.block}>
            <h2 className={styles.blockTitle}>Любимые жанры</h2>
            <div className={styles.genreList}>
              {s.top_genres.map(g => (
                <div key={g.name} className={styles.genreRow}>
                  <span className={styles.genreName}>{g.name}</span>
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
            <div className={styles.actorsGrid}>
              {s.top_actors.map(a => {
                const photo = a.profile_path ? tmdbUrl(a.profile_path, 'w185') : null
                return (
                  <Link key={a.person_id} to={`/actor/${a.person_id}`} className={styles.actorCard}>
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
