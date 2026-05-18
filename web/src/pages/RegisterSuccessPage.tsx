import { Link } from 'react-router-dom'
import styles from './AuthPage.module.scss'

export default function RegisterSuccessPage() {
  return (
    <div className={styles.page}>
      <form className={styles.card} onSubmit={e => e.preventDefault()} noValidate>
        <h1 className={styles.title}>Аккаунт создан!</h1>
        <p>Создайте первый профиль, чтобы получить API-ключ для подключения плагина.</p>
        <Link to="/profiles" className={styles.btn}>
          Создать профиль
        </Link>
      </form>
    </div>
  )
}
