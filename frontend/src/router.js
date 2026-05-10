import { createRouter, createWebHashHistory } from 'vue-router'
import NodesView from './views/NodesView.vue'

export default createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/',      redirect: '/nodes' },
    { path: '/nodes', component: NodesView },
  ],
})
