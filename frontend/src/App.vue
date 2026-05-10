<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useMainStore } from './stores/main.js'
import { onUnauthorized, setToken, getToken } from './api.js'
import * as api from './api.js'
import LogModal    from './components/LogModal.vue'
import Modal       from './components/Modal.vue'
import ToastStack  from './components/ToastStack.vue'

const store  = useMainStore()
const route  = useRoute()

// ── Status label ──────────────────────────────────────────────────────────────
const STATUS_LABEL = { running: '运行中', stopped: '已停止', error: '错误' }

const statusLabel = computed(() => STATUS_LABEL[store.status] || store.status)

const topInfo = computed(() => {
  if (store.status === 'error' && store.statusError) return { text: '⚠ ' + store.statusError, err: true }
  if (store.status === 'running' && store.activeNode)  return { text: store.activeNode.name, err: false }
  return null
})

// ── Auth modals ───────────────────────────────────────────────────────────────
const showLogin   = ref(false)
const showSetPass = ref(false)
const loginPw     = ref('')
const newPw       = ref('')

onUnauthorized(() => { showLogin.value = true })

async function doLogin() {
  try {
    const d = await api.login(loginPw.value)
    setToken(d.token)
    showLogin.value = false
    loginPw.value   = ''
    await store.refresh()
  } catch { store.toast('密码错误', 'error') }
}

function handleAuthBtn() {
  if (getToken()) showSetPass.value = true
  else            showLogin.value   = true
}

async function doSetPassword() {
  try {
    await api.setPassword(newPw.value)
    store.toast(newPw.value ? '密码已设置' : '身份验证已关闭', 'success')
    showSetPass.value = false
    if (!newPw.value) setToken('')
    newPw.value = ''
  } catch(e) { store.toast(e.message, 'error') }
}

// ── Logs modal ────────────────────────────────────────────────────────────────
const showLogs = ref(false)
function openLogs() { showLogs.value = true }

// ── Polling ───────────────────────────────────────────────────────────────────
let timer = null
async function init() {
  try {
    const a = await api.authStatus()
    if (a.hasPassword && !getToken()) { showLogin.value = true; return }
  } catch {}
  await store.refresh()
  timer = setInterval(() => store.fetchStatus().catch(() => {}), 3000)
}

onMounted(init)
onUnmounted(() => clearInterval(timer))

// ── Nav items ─────────────────────────────────────────────────────────────────
const navItems = [
  { to: '/nodes',    icon: 'nodes',    label: '节点' },
  { to: '/subs',     icon: 'subs',     label: '订阅' },
  { to: '/settings', icon: 'settings', label: '设置' },
]
</script>

<template>
  <div id="layout">

    <!-- ── Topbar ─────────────────────────────────────────────────────────── -->
    <header class="topbar">
      <span class="logo">x<em>raya</em></span>

      <div :class="['status-pill', store.status]">
        <span class="dot"></span>
        <span>{{ statusLabel }}</span>
      </div>

      <span v-if="topInfo" :class="['top-info', topInfo.err ? 'err' : '']">
        {{ topInfo.text }}
      </span>

      <div class="topbar-right">
        <button class="btn btn-ghost btn-sm" @click="openLogs">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/><polyline points="10 9 9 9 8 9"/></svg>
          日志
        </button>
        <button class="btn btn-ghost btn-sm" @click="handleAuthBtn">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0110 0v4"/></svg>
        </button>
      </div>
    </header>

    <div class="main-area">
      <!-- ── Sidebar ──────────────────────────────────────────────────────── -->
      <nav class="sidebar">
        <router-link
          v-for="item in navItems" :key="item.to"
          :to="item.to"
          class="nav-btn"
          :title="item.label"
        >
          <!-- Nodes icon -->
          <svg v-if="item.icon==='nodes'" width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="3"/>
            <circle cx="4" cy="5" r="2"/><circle cx="20" cy="5" r="2"/>
            <circle cx="4" cy="19" r="2"/><circle cx="20" cy="19" r="2"/>
            <line x1="6" y1="6" x2="10" y2="10"/><line x1="18" y1="6" x2="14" y2="10"/>
            <line x1="6" y1="18" x2="10" y2="14"/><line x1="18" y1="18" x2="14" y2="14"/>
          </svg>
          <!-- Subs icon -->
          <svg v-else-if="item.icon==='subs'" width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z"/>
          </svg>
          <!-- Settings icon -->
          <svg v-else width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <circle cx="12" cy="12" r="3"/>
            <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"/>
          </svg>
        </router-link>
      </nav>

      <!-- ── Page content ─────────────────────────────────────────────────── -->
      <main class="content">
        <router-view v-slot="{ Component }">
          <transition name="fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>

    <!-- ── Log modal ─────────────────────────────────────────────────────── -->
    <LogModal v-if="showLogs" @close="showLogs=false" />

    <!-- ── Login modal ───────────────────────────────────────────────────── -->
    <Modal v-if="showLogin" title="身份验证" small no-close>
      <div class="field">
        <label>密码</label>
        <input class="input" type="password" v-model="loginPw"
          placeholder="请输入密码" @keydown.enter="doLogin">
      </div>
      <template #foot>
        <button class="btn btn-primary" @click="doLogin">登录</button>
      </template>
    </Modal>

    <!-- ── Set password modal ────────────────────────────────────────────── -->
    <Modal v-if="showSetPass" title="设置密码" small @close="showSetPass=false">
      <p style="font-size:12px;color:var(--muted2);margin-bottom:14px">留空则关闭身份验证。</p>
      <div class="field">
        <label>新密码</label>
        <input class="input" type="password" v-model="newPw" placeholder="留空则禁用">
      </div>
      <template #foot>
        <button class="btn btn-ghost" @click="showSetPass=false">取消</button>
        <button class="btn btn-primary" @click="doSetPassword">保存</button>
      </template>
    </Modal>

    <!-- ── Toasts ─────────────────────────────────────────────────────────── -->
    <ToastStack />
  </div>
</template>

<style scoped>
#layout { display: flex; flex-direction: column; height: 100vh; overflow: hidden; }

/* ── Topbar ── */
.topbar {
  display: flex; align-items: center; gap: 12px;
  padding: 0 18px; height: 50px; min-height: 50px;
  background: var(--surface); border-bottom: 1px solid var(--border);
  position: relative; z-index: 100; flex-shrink: 0;
}
.topbar::after {
  content: ''; position: absolute; bottom: 0; left: 0; right: 0;
  height: 1px; opacity: .45;
  background: linear-gradient(90deg, var(--accent) 0%, transparent 55%);
}
.logo {
  font-family: var(--mono); font-weight: 600; font-size: 15px;
  letter-spacing: .08em; user-select: none;
}
.logo em { color: var(--accent); font-style: normal; }

.status-pill {
  display: inline-flex; align-items: center; gap: 5px;
  padding: 3px 9px; border-radius: 20px; font-size: 11px;
  font-family: var(--mono); font-weight: 500; border: 1px solid transparent;
  transition: all .25s;
}
.status-pill .dot { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.status-pill.running  { background: var(--green-bg);  color: var(--green);  border-color: var(--green-bd);  }
.status-pill.stopped  { background: var(--surface2);  color: var(--muted);  border-color: var(--border); }
.status-pill.error    { background: var(--red-bg);    color: var(--red);    border-color: var(--red-bd);    }
.status-pill.running .dot { animation: pulse 2.2s ease-in-out infinite; }
@keyframes pulse { 0%,100%{opacity:1;transform:scale(1)} 50%{opacity:.4;transform:scale(.75)} }

.top-info { font-family: var(--mono); font-size: 11px; color: var(--muted2); max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.top-info.err { color: var(--red); }
.topbar-right { margin-left: auto; display: flex; gap: 6px; align-items: center; }

/* ── Main area ── */
.main-area { display: flex; flex: 1; overflow: hidden; }

/* ── Sidebar ── */
.sidebar {
  width: 52px; background: var(--surface); border-right: 1px solid var(--border);
  display: flex; flex-direction: column; align-items: center;
  padding: 8px 0; gap: 3px; flex-shrink: 0;
}
.nav-btn {
  width: 38px; height: 38px; border-radius: 8px;
  display: flex; align-items: center; justify-content: center;
  color: var(--muted); text-decoration: none; transition: all .15s; position: relative;
}
.nav-btn:hover { background: var(--surface2); color: var(--text); }
.nav-btn.router-link-active {
  background: var(--accent-bg); color: var(--accent);
}
.nav-btn.router-link-active::before {
  content: ''; position: absolute; left: 0; top: 50%; transform: translateY(-50%);
  width: 2px; height: 18px; background: var(--accent); border-radius: 0 2px 2px 0;
}

/* ── Content ── */
.content { flex: 1; overflow-y: auto; padding: 20px; }
</style>
