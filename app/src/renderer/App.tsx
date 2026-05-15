import { useEffect } from 'react'

import { StoreProvider } from './store'
import { useStore } from './store/use-store'
import { MainScreen } from './ui/MainScreen'

function ThemedRoot() {
  const { theme } = useStore()

  useEffect(() => {
    document.documentElement.classList.toggle('theme-dark', theme === 'dark')
  }, [theme])

  return <MainScreen />
}

export default function App() {
  return (
    <StoreProvider>
      <ThemedRoot />
    </StoreProvider>
  )
}
