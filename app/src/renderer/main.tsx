import { Theme } from '@radix-ui/themes'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from './App'
import '@radix-ui/themes/styles.css'
import './styles/globals.css'

const rootElement = document.getElementById('root')

if (!rootElement) {
  throw new Error('Root element #root was not found')
}

createRoot(rootElement).render(
  <StrictMode>
    <Theme accentColor="blue" grayColor="slate" radius="medium" scaling="100%">
      <App />
    </Theme>
  </StrictMode>,
)
