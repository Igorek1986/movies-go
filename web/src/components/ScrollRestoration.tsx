import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'

export function ScrollRestoration() {
  const { key } = useLocation()

  useEffect(() => {
    window.history.scrollRestoration = 'manual'
  }, [])

  useEffect(() => {
    const saved = sessionStorage.getItem(`scroll:${key}`)
    const target = saved !== null ? parseInt(saved, 10) : 0

    let cancelRestore = false

    if (target === 0) {
      window.scrollTo(0, 0)
    } else {
      // Retry until page is tall enough for the saved position.
      // Needed when content loads asynchronously (e.g. catalog cards from API).
      const deadline = Date.now() + 3000
      function attempt() {
        if (cancelRestore) return
        const maxScroll = document.documentElement.scrollHeight - window.innerHeight
        if (maxScroll >= target || Date.now() > deadline) {
          window.scrollTo(0, target)
        } else {
          requestAnimationFrame(attempt)
        }
      }
      requestAnimationFrame(attempt)
    }

    let timer: ReturnType<typeof setTimeout>
    const save = () => {
      clearTimeout(timer)
      timer = setTimeout(() => {
        sessionStorage.setItem(`scroll:${key}`, String(Math.round(window.scrollY)))
      }, 150)
    }

    window.addEventListener('scroll', save, { passive: true })
    return () => {
      cancelRestore = true
      clearTimeout(timer)
      window.removeEventListener('scroll', save)
      sessionStorage.setItem(`scroll:${key}`, String(Math.round(window.scrollY)))
    }
  }, [key])

  return null
}
