import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhTw from 'element-plus/es/locale/lang/zh-tw'
import 'element-plus/dist/index.css'

import App from './App.vue'
import router from './router'
import { installAuthHooks } from './stores/auth'

const app = createApp(App)

app.use(createPinia())
installAuthHooks(() => {
  const current = router.currentRoute.value
  if (!current.meta.public) {
    router.replace({ name: 'login', query: { redirect: current.fullPath } })
  }
})
app.use(router)
app.use(ElementPlus, { locale: zhTw })

app.mount('#app')
