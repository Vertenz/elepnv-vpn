import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App'
import '@fontsource/geist-sans/500.css'
import '@fontsource/geist-sans/600.css'
import '@fontsource/geist-mono/500.css'
import '@fontsource/geist-mono/600.css'
import './styles/globals.css'
import './styles/components.css'

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Root element #root was not found')
}

createRoot(rootElement).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
