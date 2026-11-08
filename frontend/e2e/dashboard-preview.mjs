import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { response, settings, user } from './fixtures.ts'

const root = fileURLToPath(new URL('..', import.meta.url))
process.chdir(root)
process.env.PORTAL_UI_ACCEPTANCE = '1'
const server = await createServer({
  root,
  configFile: `${root}/vite.config.ts`,
  plugins: [{
    name: 'dashboard-populated-preview',
    configureServer(server) {
      server.middlewares.use((request, reply, next) => {
        const path = new URL(request.url, 'http://localhost').pathname
        if (!path.startsWith('/api/v1/') && path !== '/setup/status') return next()
        const data = path === '/setup/status' || path === '/api/v1/setup/status' ? { needs_setup: false } : response(path.replace(/^\/api\/v1/, ''))
        reply.setHeader('Content-Type', 'application/json')
        reply.end(JSON.stringify(path === '/api/v1/currency' ? data : { code: 0, data }))
      })
    },
    transformIndexHtml(html) {
      const init = `window.__APP_CONFIG__=${JSON.stringify(settings)};localStorage.setItem('auth_token','theme-fixture-token');localStorage.setItem('auth_user',${JSON.stringify(JSON.stringify(user))});localStorage.setItem('sub2api_locale','zh');`
      return html.replace('</head>', `<script>${init}</script></head>`)
    },
  }],
  server: { host: '127.0.0.1', port: 4181, strictPort: true },
})
await server.listen()
server.printUrls()
