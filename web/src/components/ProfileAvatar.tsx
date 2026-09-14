import { profileIconSrc, profileInitials } from '@/utils/profileIcon'

interface Props {
  icon: string
  label: string
  className?: string
}

// Профиль без выбранной иконки показывает инициалы имени, а не какую-то
// одну "дефолтную" картинку (id1) — так сразу видно, что иконка не
// выбиралась, а не что кто-то специально поставил именно эту картинку.
// Общий для ProfileSwitcher и обоих /profiles-view (Classic/Remote), см.
// CLAUDE.md — DRY, не копипастить одну и ту же картинка-или-инициалы логику.
export function ProfileAvatar({ icon, label, className }: Props) {
  if (icon) return <img className={className} src={profileIconSrc(icon)} alt="" />
  return <span className={className}>{profileInitials(label)}</span>
}
