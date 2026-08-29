const NAV_H = 52
const ROW_BOTTOM_MARGIN = 24

// Vertically center el in the area below the fixed nav bar.
export function scrollV(el: HTMLElement) {
  const rect = el.getBoundingClientRect()
  const availH = window.innerHeight - NAV_H
  const elCenter = rect.top + rect.height / 2
  const targetCenter = NAV_H + availH / 2
  window.scrollBy({ top: elCenter - targetCenter })
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
