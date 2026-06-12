import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import DOMPurify from 'dompurify'
import Layout from '@/components/Layout'
import styles from './StaticPage.module.scss'

interface Props { name: 'consent' | 'privacy' }

export default function StaticPage({ name }: Props) {
  const navigate = useNavigate()
  const [, setTitle] = useState('')
  const [html,  setHtml]  = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch(`/api/public/page?name=${name}`)
      .then(r => r.ok ? r.json() : null)
      .then(d => {
        if (d) { setTitle(d.title); setHtml(d.html); document.title = d.title }
        setLoading(false)
      })
      .catch(() => setLoading(false))
  }, [name])

  return (
    <Layout>
      <div className={styles.page}>
        {loading
          ? <p className={styles.loading}>Загрузка…</p>
          : (
            <>
              <div
                className={styles.content}
                dangerouslySetInnerHTML={{ __html: DOMPurify.sanitize(html) }}
              />
              <p className={styles.back}>
                <button className={styles.backBtn} onClick={() => navigate(-1)}>← Назад</button>
              </p>
            </>
          )
        }
      </div>
    </Layout>
  )
}
