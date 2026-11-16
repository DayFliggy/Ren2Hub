import { createPinia } from 'pinia'
import { createApp } from 'vue'

import App from './App.vue'
import i18n from './i18n'
import router, { signInDestination } from './router'
import {
  installRoutePrefetch,
  prefetchRouteAfterArrival,
} from './router/prefetch'
import { useAppStore } from './stores'
import { migrateLegacyLocalStorage } from './utils/legacyStorage'
import './styles/index.css'

migrateLegacyLocalStorage()

const app = createApp(App)
const pinia = createPinia()

app.use(pinia)
app.use(router)
app.use(i18n)

installRoutePrefetch(router)
// Nearly every sign-in continues to the console: fetch its code while the
// credentials are typed, so entering it waits only for the sign-in request.
router.afterEach((to) => {
  if (to.name === 'sign-in')
    prefetchRouteAfterArrival(router, signInDestination(to.query.redirect))
})

void useAppStore(pinia).initialize()
app.mount('#app')
