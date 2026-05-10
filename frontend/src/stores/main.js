import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as api from '../api.js'

export const useMainStore = defineStore('main', () => {
  // ── Core status ────────────────────────────────────────────────────────────
  const status      = ref('stopped') // 'running' | 'stopped' | 'error'
  const statusError = ref('')
  const activeNodeId = ref(null)

  // ── Data ──────────────────────────────────────────────────────────────────
  const nodes = ref([])
  const subs  = ref([])

  // ── Toast queue ───────────────────────────────────────────────────────────
  const toasts = ref([])
  let _toastId = 0

  // ── Computed ──────────────────────────────────────────────────────────────
  const activeNode = computed(() =>
    nodes.value.find(n => n.id === activeNodeId.value) ?? null
  )

  const isRunning = computed(() => status.value === 'running')

  function subName(groupId) {
    return subs.value.find(s => s.id === groupId)?.name ?? groupId?.slice(0, 8) ?? ''
  }

  function nodeCountOf(subId) {
    return nodes.value.filter(n => n.groupId === subId).length
  }

  // ── Actions ────────────────────────────────────────────────────────────────
  async function fetchStatus() {
    const d = await api.getStatus()
    status.value      = d.status
    statusError.value = d.error || ''
    activeNodeId.value = d.activeNode
  }

  async function fetchNodes() {
    const d = await api.listNodes()
    nodes.value = d.data ?? []
  }

  async function fetchSubs() {
    const d = await api.listSubs()
    subs.value = d.data ?? []
  }

  async function refresh() {
    await Promise.all([fetchStatus(), fetchNodes(), fetchSubs()])
  }

  // ── Toast ──────────────────────────────────────────────────────────────────
  function toast(msg, type = 'info') {
    const id = ++_toastId
    toasts.value.push({ id, msg, type, leaving: false })
    setTimeout(() => {
      const t = toasts.value.find(x => x.id === id)
      if (t) t.leaving = true
      setTimeout(() => {
        toasts.value = toasts.value.filter(x => x.id !== id)
      }, 300)
    }, 3200)
  }

  return {
    status, statusError, activeNodeId, activeNode, isRunning,
    nodes, subs, toasts,
    subName, nodeCountOf,
    fetchStatus, fetchNodes, fetchSubs, refresh,
    toast,
  }
})
