import { Link } from 'react-router-dom'
import styles from './AuthPage.module.scss'

export default function NotFoundPage() {
  return (
    <div className={styles.page}>
      <div className={styles.card}>
        <h1 className={styles.title} style={{ fontSize: '4rem', marginBottom: 0 }}>404</h1>
        <p className={styles.subtitle}>Страница не найдена</p>
        <Link to="/catalog" className={styles.btn} style={{ textAlign: 'center', display: 'flex', alignItems: 'center', justifyContent: 'center', textDecoration: 'none' }}>
          На главную
        </Link>
      </div>
    </div>
  )
}
