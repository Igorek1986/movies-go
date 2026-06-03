import { Component, type ReactNode } from 'react'
import styles from '@/pages/AuthPage.module.scss'

interface Props { children: ReactNode }
interface State { error: Error | null }

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div className={styles.page}>
          <div className={styles.card}>
            <h1 className={styles.title}>Что-то пошло не так</h1>
            <p className={styles.subtitle}>{this.state.error.message}</p>
            <button className={styles.btn} onClick={() => window.location.reload()}>
              Обновить страницу
            </button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
