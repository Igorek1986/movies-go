import { useEffect, useRef } from 'react'
import { getUnsavedChangesGuard } from '@/utils/unsavedChangesGuard'
import { softRefresh, canRefreshInPlace } from '@/utils/softRefresh'
import { isNetworkQuiet } from '@/utils/networkActivity'
import styles from './PullToRefresh.module.scss'

// Страница едет за пальцем с нарастающим сопротивлением, но не дальше MAX_PULL.
// Отпустили за THRESHOLD — обновляем.
const THRESHOLD = 64
const MAX_PULL = 100
// Начальный наклон «резинки» — чем дальше тянешь, тем сильнее она сопротивляется
// и плавно упирается в MAX_PULL (см. rubberBand), а не обрывается о потолок
const RESISTANCE = 0.6
// Сколько px пальца игнорируем, прежде чем понять «вертикаль или горизонталь» —
// иначе горизонтальный свайп по карусели с первого же пикселя тянул бы страницу.
const DIRECTION_LOCK_PX = 8
// Сколько страница висит оттянутой, пока идёт обновление. Равно THRESHOLD:
// отпустил чуть за порогом — страница вообще не дёргается, только если тянул
// сильно дальше, подъезжает к этой позиции.
const HOLD_PULL = THRESHOLD
// Подтяжка к HOLD_PULL после отпускания — ремаунт стартует только когда она
// закончилась, иначе новая страница вставала бы на место мгновенно, без анимации
const SETTLE_MS = 300
// Возврат страницы наверх — дольше и с плавным стартом И торможением: прежний
// ease-out с резким стартом читался как «выстрел» вверх
const RELEASE_MS = 550
// После окончания transition ещё немного ждём, прежде чем снимать inline-стили:
// таймер стартует раньше, чем сам transition реально закончится
const CLEAR_MARGIN_MS = 60
// Снимок старой страницы растворяется, открывая новую, за FADE_MS
const FADE_MS = 320
// Сколько «тишины» в сети считаем признаком, что новая страница дозагрузилась
const QUIET_MS = 250
const EASE_SETTLE = 'cubic-bezier(0.25, 0.8, 0.25, 1)'
const EASE_RELEASE = 'cubic-bezier(0.45, 0, 0.2, 1)'
// Минимум, сколько держим спиннер после ремаунта: данные успевают подтянуться
// под ним, а не «выстреливают» в момент, когда страница уже вернулась на место.
const MIN_REFRESH_MS = 700
// Потолок ожидания данных (на месте или под снимком) — не зависаем на вечном polling'е
const MAX_REFRESH_MS = 8000
// Сколько максимум ждём декодирования картинок снимка, прежде чем показать его как есть
const SNAPSHOT_READY_CAP_MS = 600

// Есть ли у элемента или его предков вертикальный скролл, который уже не в
// самом верху (выдвижное меню, модалки с overflow-y) — тогда жест принадлежит
// им, а не обновлению страницы.
function insideScrolledContainer(el: Element | null): boolean {
  for (let n: Element | null = el; n && n !== document.documentElement; n = n.parentElement) {
    if (n.scrollTop > 0) {
      const oy = getComputedStyle(n).overflowY
      if (oy === 'auto' || oy === 'scroll') return true
    }
  }
  return false
}

// Экспоненциальная «резинка»: начальный наклон RESISTANCE, асимптота MAX_PULL
function rubberBand(dy: number): number {
  return MAX_PULL * (1 - Math.exp(-(dy * RESISTANCE) / MAX_PULL))
}

const wait = (ms: number) => new Promise<void>(r => setTimeout(r, ms))
const nextFrames = (n: number) => new Promise<void>(r => {
  const step = (left: number) => requestAnimationFrame(() => (left <= 1 ? r() : step(left - 1)))
  step(n)
})

// Картинки в пределах экрана и небольшого запаса под ним — только их ждём и
// форсируем: у постеров loading="lazy", а на длинной «Истории» их сотни.
function nearViewport(img: HTMLImageElement): boolean {
  const r = img.getBoundingClientRect()
  return r.bottom > 0 && r.top < window.innerHeight * 1.5
}

// loading="lazy" + свежесозданный <img> (ремаунт или клон) = браузер сначала
// откладывает загрузку, потом декодирует асинхронно — на кадр-другой место
// постера пустое, это и было морганием. eager запускает загрузку сразу, decode()
// дожидается, пока картинка реально готова к показу.
function warmImage(img: HTMLImageElement): Promise<void> {
  if (img.loading === 'lazy') img.loading = 'eager'
  return img.decode().catch(() => { /* битая картинка — не ждём вечно */ })
}

// Готовность картинок новой страницы под снимком: каждой найденной ещё не
// виденной картинке запускаем warmImage и на этот проход считаем страницу
// неготовой; следующий опрос увидит уже результат.
const warmed = new WeakMap<HTMLImageElement, boolean>()
function newPageImagesReady(): boolean {
  let ready = true
  for (const img of document.querySelectorAll<HTMLImageElement>('[data-ptr-move] img')) {
    if (!nearViewport(img)) continue
    const done = warmed.get(img)
    if (done === undefined) {
      warmed.set(img, false)
      warmImage(img).then(() => warmed.set(img, true))
      ready = false
    } else if (!done) {
      ready = false
    }
  }
  return ready
}

// Статичная копия текущей страницы поверх экрана. Ремаунт неизбежно на
// мгновение показывает пустые/«Загрузка…» состояния и перерисовывает постеры —
// это и было морганием; под снимком всё это происходит незаметно, а потом он
// плавно растворяется. inert + pointer-events: none — снимок только картинка.
// Создаётся невидимым (opacity 0): показать его можно только когда его
// собственные картинки уже декодированы (см. prepareSnapshot), иначе он сам
// начал бы моргать пустыми постерами в момент появления.
function takeSnapshot(cls: string): HTMLElement | null {
  const root = document.querySelector<HTMLElement>('[data-ptr-root]')
  if (!root) return null
  const rect = root.getBoundingClientRect()
  const clone = root.cloneNode(true) as HTMLElement
  // Не должно попасть в document.querySelector ни PullToRefresh'а, ни страниц
  clone.removeAttribute('data-ptr-root')
  clone.querySelectorAll('[data-ptr-move]').forEach(el => el.removeAttribute('data-ptr-move'))
  clone.querySelectorAll('[id]').forEach(el => el.removeAttribute('id'))
  const wrap = document.createElement('div')
  wrap.className = cls
  wrap.setAttribute('aria-hidden', 'true')
  wrap.setAttribute('inert', '')
  wrap.style.opacity = '0'
  wrap.style.top = `${rect.top + window.scrollY}px`
  wrap.style.width = `${rect.width}px`
  wrap.appendChild(clone)
  document.body.appendChild(wrap)
  return wrap
}

// Ждёт, пока картинки снимка (в пределах экрана) готовы; потолок — чтобы медленная
// сеть не задерживала обновление
function prepareSnapshot(wrap: HTMLElement): Promise<void> {
  const imgs = Array.from(wrap.querySelectorAll<HTMLImageElement>('img')).filter(nearViewport)
  return Promise.race([Promise.all(imgs.map(warmImage)).then(() => {}), wait(SNAPSHOT_READY_CAP_MS)])
}

// Pull-to-refresh для тач-интерфейса: в iOS-PWA (standalone) нативного нет
// вообще, в Android Chrome оно есть, но перезагружает всё вслепую — свой
// жест ведёт себя одинаково везде. Тянется сам контент страницы ([data-ptr-move],
// его вешает Layout на <main>) — верхняя и нижняя панели стоят на месте, в
// освободившемся под верхней панелью зазоре виден спиннер. Обновление — мягкое
// (utils/softRefresh.ts: ремаунт страницы + сброс кешей), не location.reload():
// полная перезагрузка на миг оставляла пустой экран, отсюда было мигание.
export function PullToRefresh() {
  const spinnerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const spinner = spinnerRef.current
    if (!spinner) return

    let startX = 0
    let startY = 0
    let pull = 0
    let targets: HTMLElement[] = []
    // idle — жест не наш; tracking — палец у верха, направление ещё не ясно;
    // pulling — вертикальное движение вниз, страницу ведём мы; done — уже перезагружаемся
    let mode: 'idle' | 'tracking' | 'pulling' | 'done' = 'idle'

    // ms = 0 — без анимации (палец ведёт страницу напрямую)
    function render(ms: number, ease = EASE_RELEASE) {
      const tr = ms ? `transform ${ms}ms ${ease}, opacity ${ms}ms ${ease}` : 'none'
      for (const t of targets) {
        t.style.transition = tr
        // Пустое значение (не translateY(0)) — иначе transform так и остался бы
        // и делал контент containing block'ом для position:fixed внутри страницы
        t.style.transform = pull > 0 ? `translateY(${pull}px)` : ''
      }
      // Спиннер едет в такт странице (центр зазора под верхней панелью), а гаснет
      // вдвое быстрее: к моменту, когда зазор стал меньше самого колеса, его уже нет
      spinner!.style.transition = ms
        ? `transform ${ms}ms ${ease}, opacity ${Math.round(ms / 2)}ms ease-out`
        : 'none'
      spinner!.style.opacity = String(Math.min(pull / HOLD_PULL, 1))
      spinner!.style.transform = `translateY(${pull / 2}px)`
      // Угол — в CSS-переменную: keyframes спиннера (PullToRefresh.module.scss)
      // стартуют с него, поэтому вращение подхватывается с того же места без
      // скачка. Но только пока ведёт палец: когда спиннер уже крутится (mode
      // 'done'), смена переменной пересчитала бы keyframes на лету и повернула
      // колесо рывком.
      const icon = spinner!.firstElementChild as HTMLElement | null
      if (icon && mode !== 'done') icon.style.setProperty('--ptr-rot', `${pull * 4}deg`)
    }

    function clearTargets(prev: HTMLElement[]) {
      for (const t of prev) { t.style.transition = ''; t.style.transform = ''; t.style.opacity = '' }
    }

    function reset() {
      if (mode === 'done') return
      mode = 'idle'
      pull = 0
      render(RELEASE_MS)
      const prev = targets
      // Не трогаем, если за это время уже начался новый жест
      setTimeout(() => { if (mode === 'idle' && pull === 0) clearTargets(prev) }, RELEASE_MS + CLEAR_MARGIN_MS)
    }

    function onStart(e: TouchEvent) {
      if (mode === 'done') return
      mode = 'idle'
      if (e.touches.length !== 1) return
      // Меню открыто — Layout блокирует body через overflow: hidden
      if (document.body.style.overflow === 'hidden') return
      if (window.scrollY > 0) return
      if (insideScrolledContainer(e.target as Element | null)) return
      targets = Array.from(document.querySelectorAll<HTMLElement>('[data-ptr-move]'))
      // Нет что двигать (логин и прочие страницы без Layout) — жест не нужен
      if (!targets.length) return
      const t = e.touches[0]
      startX = t.clientX
      startY = t.clientY
      mode = 'tracking'
    }

    function onMove(e: TouchEvent) {
      if (mode !== 'tracking' && mode !== 'pulling') return
      const t = e.touches[0]
      const dx = t.clientX - startX
      const dy = t.clientY - startY

      if (mode === 'tracking') {
        if (Math.abs(dx) < DIRECTION_LOCK_PX && Math.abs(dy) < DIRECTION_LOCK_PX) return
        // Вверх, вбок или страницу уже проскроллили за время жеста — не наш
        if (dy <= 0 || Math.abs(dx) > Math.abs(dy) || window.scrollY > 0) { mode = 'idle'; return }
        mode = 'pulling'
      }

      if (window.scrollY > 0 || dy <= 0) { reset(); return }
      // Гасим нативный overscroll/PTR браузера — иначе он тянул бы параллельно с нашим
      if (e.cancelable) e.preventDefault()
      pull = rubberBand(dy)
      render(0)
    }

    function onEnd() {
      if (mode !== 'pulling') { mode = 'idle'; return }
      if (pull < THRESHOLD) { reset(); return }
      // Несохранённые правки в настройках — не теряем молча
      if (getUnsavedChangesGuard()?.isDirty()) { reset(); return }
      mode = 'done'
      // Сначала плавно подтягиваем страницу к позиции удержания, спиннер уже крутится
      pull = HOLD_PULL
      render(SETTLE_MS, EASE_SETTLE)
      const inPlace = canRefreshInPlace()
      spinner!.dataset.loading = 'true'
      // Обновление на месте ничего не заменяет — снимок не нужен. Иначе снимок
      // снимается ДО ремаунта, пока старая страница ещё на экране, и пока идёт
      // подтяжка к позиции удержания готовит свои картинки
      const snapshot = inPlace ? null : takeSnapshot(styles.snapshot)
      Promise.all([wait(SETTLE_MS), snapshot ? prepareSnapshot(snapshot) : null]).then(async () => {
        if (snapshot) {
          // Снимок один в один повторяет страницу под ним, поэтому появляется
          // незаметно; пара кадров — чтобы он гарантированно успел отрисоваться
          // до ремаунта
          snapshot.style.opacity = '1'
          await nextFrames(2)
        }
        refresh(inPlace, snapshot)
      })
    }

    function refresh(inPlace: boolean, snapshot: HTMLElement | null) {
      const end = Date.now() + MIN_REFRESH_MS
      const cap = Date.now() + MAX_REFRESH_MS
      let settled = false
      softRefresh().catch(() => {}).finally(() => { settled = true })
      // Ремаунт заменил <main> новым элементом — без сдвига (и без transition:
      // он должен встать ровно туда же, где был старый) страница дёрнулась бы
      // вверх. Страница с ленивой отрисовкой Layout может появиться позже —
      // поэтому цикл до конца удержания, а не разовый поиск.
      function hold() {
        targets = Array.from(document.querySelectorAll<HTMLElement>('[data-ptr-move]'))
        for (const t of targets) {
          if (t.style.transform) continue
          t.style.transition = 'none'
          t.style.transform = `translateY(${HOLD_PULL}px)`
        }
        const now = Date.now()
        // На месте — ждём сами данные (promise), под снимком — пока новая
        // страница не дозагрузится (сеть затихла, видимые постеры на месте);
        // и то и другое не дольше потолка
        // Картинки прогреваем сразу, как только появились, а не после тишины в сети
        const imagesReady = inPlace || newPageImagesReady()
        const ready = inPlace ? settled : isNetworkQuiet(QUIET_MS) && imagesReady
        if (now < end || (!ready && now < cap)) { requestAnimationFrame(hold); return }
        if (snapshot) dissolve(snapshot)
        else release()
      }
      function dissolve(el: HTMLElement) {
        el.style.transition = `opacity ${FADE_MS}ms ease`
        el.style.opacity = '0'
        setTimeout(() => { el.remove(); release() }, FADE_MS)
      }
      function release() {
        pull = 0
        render(RELEASE_MS)
        // Спиннер продолжает крутиться, пока гаснет и уезжает под панель
        setTimeout(() => {
          delete spinner!.dataset.loading
          clearTargets(targets)
          mode = 'idle'
        }, RELEASE_MS + CLEAR_MARGIN_MS)
      }
      hold()
    }

    document.addEventListener('touchstart', onStart, { passive: true })
    // passive: false — иначе preventDefault в onMove игнорируется
    document.addEventListener('touchmove', onMove, { passive: false })
    document.addEventListener('touchend', onEnd, { passive: true })
    document.addEventListener('touchcancel', reset, { passive: true })
    return () => {
      document.removeEventListener('touchstart', onStart)
      document.removeEventListener('touchmove', onMove)
      document.removeEventListener('touchend', onEnd)
      document.removeEventListener('touchcancel', reset)
    }
  }, [])

  return (
    <div ref={spinnerRef} className={styles.spinner} aria-hidden="true">
      <svg className={styles.icon} width="26" height="26" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
        <path d="M12 3a9 9 0 1 0 9 9" />
      </svg>
    </div>
  )
}
