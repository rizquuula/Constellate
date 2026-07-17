import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
// Self-hosted IBM Plex (no external Google Fonts call — works offline/air-gapped).
// Weights 400/500/600 match the families referenced by --font-mono / --font-sans.
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import '@fontsource/ibm-plex-mono/600.css'
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@xterm/xterm/css/xterm.css'
import './styles.css'
import { App } from './App'

const root = document.getElementById('root')
if (!root) throw new Error('no #root element')
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>
)

// Register the PWA service worker for installability + offline shell fallback.
if ('serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}
