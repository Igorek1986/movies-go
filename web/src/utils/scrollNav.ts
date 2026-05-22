const NAV_H = 52

// Vertically center el in the area below the fixed nav bar.
export function scrollV(el: HTMLElement) {
  const rect = el.getBoundingClientRect()
  const availH = window.innerHeight - NAV_H
  const elCenter = rect.top + rect.height / 2
  const targetCenter = NAV_H + availH / 2
  window.scrollBy({ top: elCenter - targetCenter })
}

// Count columns in a CSS grid by comparing getBoundingClientRect().top of items.
export function getGridCols(cards: HTMLElement[]): number {
  if (cards.length < 2) return 1
  const firstTop = cards[0].getBoundingClientRect().top
  let cols = 1
  for (let i = 1; i < cards.length; i++) {
    if (Math.abs(cards[i].getBoundingClientRect().top - firstTop) > 5) break
    cols++
  }
  return cols
}
