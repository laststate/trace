import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './styles.css'

// OIDC callback token in hash
if (location.hash.startsWith('#token=')) {
  const t = decodeURIComponent(location.hash.slice(7))
  localStorage.setItem('trace_session', t)
  history.replaceState(null, '', location.pathname)
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
