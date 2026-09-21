// Реестр страниц, умеющих обновиться pull-to-refresh'ем «на месте» — без
// ремаунта, тихо подменяя данные (карточки Каталога остаются в DOM, постеры не
// перерисовываются, ничего не мигает). Отдельный файл от softRefresh.ts, чтобы
// страницы могли регистрироваться, не создавая циклического импорта
// (softRefresh.ts сам импортирует страницы ради сброса их кешей).
export interface InPlaceRefresh {
  // Может ли страница сейчас обновиться на месте (нет раскрытой категории/
  // поиска и т.п.) — иначе softRefresh откатывается к ремаунту
  canRefresh: () => boolean
  // Resolve — когда свежие данные уже показаны
  refresh: () => Promise<void>
}

const handlers = new Set<InPlaceRefresh>()

export function registerInPlaceRefresh(h: InPlaceRefresh): () => void {
  handlers.add(h)
  return () => { handlers.delete(h) }
}

export function findInPlaceRefresh(): InPlaceRefresh | null {
  for (const h of handlers) if (h.canRefresh()) return h
  return null
}
