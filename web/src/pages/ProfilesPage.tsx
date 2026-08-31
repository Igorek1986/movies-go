import { lazy, Suspense } from 'react'
import { useAuth } from '@/hooks/useAuth'
import { resolveSettingsLayout } from '@/utils/settingsLayout'

// Thin switcher between the two /profiles renderings — see
// utils/settingsLayout.ts. Both views call useProfilesPageState()
// themselves (it's cheap, no heavy JSX of its own), so this component just
// decides which one to mount and lazy-loads it, keeping a 'classic' user
// from ever downloading ProfilesRemoteView + components/remote/* and vice
// versa.
const ProfilesClassicView = lazy(() => import('./profiles/ProfilesClassicView'))
const ProfilesRemoteView = lazy(() => import('./profiles/ProfilesRemoteView'))

export default function ProfilesPage() {
  const { user, loading } = useAuth()
  // Waits for the account's settings_layout to load before picking a view —
  // it's server-side now (see users.settings_layout), so there's no
  // synchronous localStorage fallback to render on the first paint.
  if (loading) return null
  const layout = resolveSettingsLayout(user?.settings_layout)
  return (
    <Suspense fallback={null}>
      {layout === 'remote' ? <ProfilesRemoteView /> : <ProfilesClassicView />}
    </Suspense>
  )
}
