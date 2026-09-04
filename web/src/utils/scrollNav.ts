// Must match $nav-height in variables.scss.
export const NAV_H = 52
const ROW_BOTTOM_MARGIN = 24

// Drum-carousel row switch (CatalogPage/MediaLibraryPage's `transition`
// state) — how long the outgoing row stays mounted before it's dropped.
// Must match $carousel-transition-ms in variables.scss; shared here instead
// of duplicated as a literal in both pages so the two can't drift apart.
export const CAROUSEL_TRANSITION_MS = 280

// Hero carousel wheel handling (CatalogPage/MediaLibraryPage) — shared here
// for the same reason as CAROUSEL_TRANSITION_MS above, not duplicated as a
// literal in both pages. A single physical wheel notch/trackpad swipe fires
// many wheel events in quick succession; without a cooldown each one moved
// focus/switched a category again, reading as wildly oversensitive. Card
// focus has no animation to wait out (an instant scrollH), so it gets its
// own shorter value instead of reusing CAROUSEL_TRANSITION_MS; the category
// switch one is deliberately longer than that transition's own duration —
// this throttles perceived scroll speed, not just deduplicates one gesture.
export const CARD_WHEEL_COOLDOWN_MS = 350
export const CATEGORY_WHEEL_COOLDOWN_MS = 450

// Holding an arrow key fires native OS key-repeat keydowns much faster than
// scrollH/scrollV's smooth-scroll animation can finish — each one retargets
// the in-flight scroll, which reads as the row/grid jerking or stuttering
// instead of gliding. `cooldownRef` is a caller-owned ref (arrow move
// handlers live in two different components, CatalogPage and
// MediaLibraryPage, so this can't hold its own module-level state) that
// persists the last-processed repeat's timestamp across renders. A real
// first press (e.repeat === false) always goes through immediately; only
// the auto-repeat stream gets throttled. Reuses CARD_WHEEL_COOLDOWN_MS
// rather than its own constant — same goal (don't let rapid repeats queue
// up faster than one move reads as complete), no reason for a different one.
export function shouldThrottleKeyRepeat(e: { repeat: boolean }, cooldownRef: { current: number }): boolean {
  const now = Date.now()
  if (!e.repeat) {
    cooldownRef.current = now
    return false
  }
  if (now - cooldownRef.current < CARD_WHEEL_COOLDOWN_MS) return true
  cooldownRef.current = now
  return false
}

// Vertically center el in the area below the fixed nav bar. Animated (not
// an instant jump) — every caller is a keyboard-driven focus move (between
// cards/rows in Classic layout, or search results), where an un-animated
// page-level scroll reads as the view snapping/jerking instead of following
// the focus move. html's own scroll-behavior is 'auto' (see global.scss),
// so this has to opt in per call, same as scrollToRowBottom below already does.
export function scrollV(el: HTMLElement) {
  const rect = el.getBoundingClientRect()
  const availH = window.innerHeight - NAV_H
  const elCenter = rect.top + rect.height / 2
  const targetCenter = NAV_H + availH / 2
  window.scrollBy({ top: elCenter - targetCenter, behavior: 'smooth' })
}

// Scroll a row's horizontal scroll container so el is centered — shared by
// CatalogPage and MediaLibraryPage, whose rows both mark their scroll
// container with data-row-scroll. `instant` is for restoring a row to where
// it should already be (e.g. back from an expanded category) rather than
// animating a real move — Chrome applies the container's own
// `scroll-behavior: smooth` (see .rowScroll) to a plain `scrollLeft`
// assignment too, not just scrollTo, so getting a real instant jump means
// toggling scroll-behavior off for that one assignment and restoring it
// right after, rather than just skipping scrollTo.
export function scrollH(el: HTMLElement, instant = false) {
  const scroll = el.closest<HTMLElement>('[data-row-scroll]')
  if (!scroll) return
  const sr = scroll.getBoundingClientRect()
  const cr = el.getBoundingClientRect()
  const relCenter = cr.left - sr.left + scroll.scrollLeft + cr.width / 2
  const left = relCenter - scroll.clientWidth / 2
  if (instant) {
    const prevBehavior = scroll.style.scrollBehavior
    scroll.style.scrollBehavior = 'auto'
    scroll.scrollLeft = left
    scroll.style.scrollBehavior = prevBehavior
  } else {
    scroll.scrollTo({ left })
  }
}

// Pin el to the bottom of the viewport (with a small margin) instead of
// centering it — used for row-to-row navigation on Catalog/Моё so the
// active row always sits in the same place (just above the fold), matching
// where it lands on first load, rather than jumping to mid-screen. Animated
// (unlike scrollV/scrollH's instant jumps) since an un-animated page-level
// scroll here reads as the current row abruptly vanishing and the next one
// snapping into its place instead of a continuous move.
export function scrollToRowBottom(el: HTMLElement) {
  const rect = el.getBoundingClientRect()
  const targetBottom = window.innerHeight - ROW_BOTTOM_MARGIN
  window.scrollBy({ top: rect.bottom - targetBottom, behavior: 'smooth' })
}

// Focuses the top nav's active page link (e.g. "Каталог" while on /catalog)
// when ArrowUp bridges up from Catalog/МедиаLibrary content into the menu —
// falls back to the search button only if no link is marked active (a page
// with no matching top-nav item at all). NavLink sets aria-current="page"
// on the active link itself, so this doesn't need to know the current route.
export function focusTopNavActive() {
  const el = document.querySelector<HTMLElement>('[data-top-nav] a[aria-current="page"]')
    ?? document.querySelector<HTMLElement>('[data-top-nav] [data-top-nav-search]')
  el?.focus()
}

// Count columns in a CSS grid by comparing offsetTop of items — not
// getBoundingClientRect().top, which includes CSS transforms. The focused
// (or hovered) card lifts by translateY(-5px) (see .card:focus-visible/
// :hover), which is enough to tip the old rect-based comparison's 5px
// tolerance and misread a full row as 1 column the moment cards[0] itself
// is the shifted one. offsetTop reflects layout position only, so it's
// unaffected by that visual lift.
export function getGridCols(cards: HTMLElement[]): number {
  if (cards.length < 2) return 1
  const firstTop = cards[0].offsetTop
  let cols = 1
  for (let i = 1; i < cards.length; i++) {
    if (Math.abs(cards[i].offsetTop - firstTop) > 5) break
    cols++
  }
  return cols
}
