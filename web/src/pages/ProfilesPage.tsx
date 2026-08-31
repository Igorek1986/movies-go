import { lazy, Suspense } from 'react'
import { getStoredSettingsLayout } from '@/utils/settingsLayout'

// Thin switcher between the two /profiles renderings — see
// utils/settingsLayout.ts. Both views call useProfilesPageState()
// themselves (it's cheap, no heavy JSX of its own), so this component just
// decides which one to mount and lazy-loads it, keeping a 'classic' user
// from ever downloading ProfilesRemoteView + components/remote/* and vice
// versa.
const ProfilesClassicView = lazy(() => import('./profiles/ProfilesClassicView'))
const ProfilesRemoteView = lazy(() => import('./profiles/ProfilesRemoteView'))

export default function ProfilesPage() {
  const layout = getStoredSettingsLayout()
  return (
    <Suspense fallback={null}>
      {layout === 'remote' ? <ProfilesRemoteView /> : <ProfilesClassicView />}
    </Suspense>
  )
}
