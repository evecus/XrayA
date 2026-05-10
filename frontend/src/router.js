import { createRouter, createWebHashHistory } from 'vue-router'
import NodesView    from './views/NodesView.vue'
import SubsView     from './views/SubsView.vue'
import SettingsView from './views/SettingsView.vue'

export default createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/',         redirect: '/nodes' },
    { path: '/nodes',    component: NodesView    },
    { path: '/subs',     component: SubsView     },
    { path: '/settings', component: SettingsView },
  ],
})
