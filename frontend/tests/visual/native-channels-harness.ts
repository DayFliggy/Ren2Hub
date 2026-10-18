import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import ChannelsView from '../../src/views/console/ChannelsView.vue'
import { useAuthStore } from '../../src/stores/auth'
import type { UserInfo } from '../../src/types/auth'
import i18n, { loadMessageDomain, setLocale } from '../../src/i18n'
import '../../src/styles/index.css'

await loadMessageDomain('console')
await setLocale('zh-CN')
const app = createApp({ render: () => h(ChannelsView) })
const pinia = createPinia()
app.use(pinia).use(i18n)
const auth = useAuthStore(pinia)
auth.user = {
  id: 1,
  username: 'channel-fixture',
  role: 100,
  status: 1,
} as UserInfo
Object.assign(window, { channelFixtureAuth: auth })
app.mount('#app')
