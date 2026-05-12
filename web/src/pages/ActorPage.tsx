import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import Layout from '@/components/Layout'
import { tmdbUrl } from '@/utils/poster'
import styles from './ActorPage.module.scss'

interface ActorWork {
  id: number; media_type: string; title: string; poster_path: string
  year: string; character: string
}

interface ActorData {
  id: number; name: string; biography: string; birthday: string
  profile_path: string | null; works: ActorWork[]
}

export default function ActorPage() {
  const { personId } = useParams<{ personId: string }>()
  const navigate = useNavigate()
  const [actor, setActor] = useState<ActorData | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    if (!personId) return
    setLoading(true)
    fetch(`/api/actor/${personId}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => { setActor(d); setLoading(false) })
      .catch(() => setLoading(false))
  }, [personId])

  const photoUrl = actor?.profile_path ? tmdbUrl(actor.profile_path, 'w185') : null

  if (loading) return <Layout><div className={styles.loading}>Загрузка…</div></Layout>
  if (!actor) return (
    <Layout>
      <button className={styles.floatBack} onClick={() => navigate(-1)}>← Назад</button>
      <div className={styles.notFound}><p>Не найдено</p></div>
    </Layout>
  )

  return (
    <Layout>
      <button className={styles.floatBack} onClick={() => navigate(-1)}>← Назад</button>
      <div className={styles.page}>

        <div className={styles.hero}>
          <div className={styles.photoWrap}>
            {photoUrl
              ? <img className={styles.photo} src={photoUrl} alt={actor.name} />
              : <div className={styles.photoPlaceholder}>👤</div>
            }
          </div>
          <div className={styles.info}>
            <h1 className={styles.name}>{actor.name}</h1>
            {actor.birthday && <p className={styles.birthday}>Дата рождения: {actor.birthday}</p>}
            {actor.biography && <p className={styles.biography}>{actor.biography}</p>}
          </div>
        </div>

        {actor.works.length > 0 ? (
          <section className={styles.section}>
            <h2 className={styles.sectionTitle}>Фильмография</h2>
            <div className={styles.grid}>
              {actor.works.map(item => {
                const cardId = `${item.id}_${item.media_type}`
                const poster = item.poster_path ? tmdbUrl(item.poster_path, 'w342') : null
                return (
                  <Link
                    key={item.id}
                    to={`/card/${encodeURIComponent(cardId)}`}
                    className={styles.card}
                  >
                    {poster
                      ? <img className={styles.poster} src={poster} alt={item.title} loading="lazy" />
                      : <div className={styles.posterPlaceholder}>{item.title}</div>
                    }
                    {item.media_type === 'tv' && <span className={styles.badge}>СЕРИАЛ</span>}
                    <div className={styles.cardBody}>
                      <p className={styles.cardTitle}>{item.title}</p>
                      {item.year && <p className={styles.year}>{item.year}</p>}
                      {item.character && <p className={styles.character}>{item.character}</p>}
                    </div>
                  </Link>
                )
              })}
            </div>
          </section>
        ) : (
          <p className={styles.empty}>Фильмография не найдена</p>
        )}
      </div>
    </Layout>
  )
}
