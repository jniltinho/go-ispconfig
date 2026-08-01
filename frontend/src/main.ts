// Frontend entrypoint: Vue 3 + Pinia + vue-router, styles with the
// ISPConfig3-derived theme tokens.
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import { router } from './router'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
