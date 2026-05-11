<script setup>
import { ref, onMounted } from 'vue'
import { useMainStore } from '../stores/main.js'
import * as api from '../api.js'

const store   = useMainStore()
const saving  = ref(false)

// Local copy of settings — avoids mutating store directly
const form = ref({
  proxyMode:   'socks5',
  routeMode:   'whitelist',
  socks5Port:  20170,
  httpPort:    20171,
  tproxyPort:  52345,
  dnsPort:     15353,
  dnsUpstream: '8.8.8.8',
  dnsLocal:    '114.114.114.114',
  routingA:    '',
  sniffing:    true,
  ipv6:        false,
})

async function loadSettings() {
  try {
    const d = await api.getSettings()
    if (d.data) Object.assign(form.value, d.data)
  } catch {}
}

async function saveSettings() {
  saving.value = true
  try {
    await api.putSettings(form.value)
    store.toast('设置已保存', 'success')
  } catch(e) {
    store.toast(e.message, 'error')
  } finally { saving.value = false }
}

onMounted(loadSettings)
</script>

<template>
  <div>
    <div class="card">
      <div class="card-head">设置</div>
      <div class="card-body" style="padding: 22px">

        <!-- Proxy mode section -->
        <div class="settings-section">
          <div class="section-title">代理模式</div>
          <div class="settings-grid">
            <div class="field">
              <label>模式</label>
              <select class="select" v-model="form.proxyMode">
                <option value="socks5">Socks5 / HTTP</option>
                <option value="tproxy">TProxy（透明 TCP+UDP）</option>
                <option value="redir">Redirect（透明 TCP）</option>
              </select>
            </div>
            <div class="field">
              <label>Socks5 端口</label>
              <input class="input" type="number" v-model.number="form.socks5Port">
            </div>
            <div class="field">
              <label>HTTP 端口</label>
              <input class="input" type="number" v-model.number="form.httpPort">
            </div>
            <div class="field">
              <label>TProxy / Redir 端口</label>
              <input class="input" type="number" v-model.number="form.tproxyPort">
            </div>
          </div>
        </div>

        <!-- Route section -->
        <div class="settings-section">
          <div class="section-title">路由</div>
          <div class="field">
            <label>路由模式</label>
            <select class="select" v-model="form.routeMode">
              <option value="whitelist">大陆白名单（代理非CN流量）</option>
              <option value="blacklist">GFW黑名单（仅代理被封锁的）</option>
              <option value="routingA">自定义 RoutingA</option>
            </select>
          </div>
          <div v-if="form.routeMode === 'routingA'" class="field">
            <label>RoutingA 规则</label>
            <textarea
              class="input" v-model="form.routingA" rows="9"
              placeholder="# RoutingA 语法&#10;default: proxy&#10;domain(geosite:cn) -> direct&#10;ip(geoip:cn) -> direct"
            ></textarea>
          </div>
        </div>

        <!-- DNS section -->
        <div class="settings-section">
          <div class="section-title">DNS</div>
          <div class="settings-grid">
            <div class="field">
              <label>境外 DNS（走代理）</label>
              <input class="input" v-model="form.dnsUpstream" placeholder="8.8.8.8">
            </div>
            <div class="field">
              <label>本地 DNS（直连）</label>
              <input class="input" v-model="form.dnsLocal" placeholder="114.114.114.114">
            </div>
            <div class="field" v-if="form.proxyMode !== 'socks5'">
              <label>DNS 劫持端口</label>
              <input class="input" type="number" v-model.number="form.dnsPort" placeholder="15353">
            </div>
          </div>
        </div>

        <!-- Options section -->
        <div class="settings-section">
          <div class="section-title">选项</div>
          <label class="check-row">
            <input type="checkbox" v-model="form.sniffing">
            <span>启用流量嗅探（域名检测）</span>
          </label>
          <label class="check-row">
            <input type="checkbox" v-model="form.ipv6">
            <span>启用 IPv6</span>
          </label>
        </div>

        <div style="display:flex;align-items:center;gap:12px">
          <button class="btn btn-primary" :disabled="saving" @click="saveSettings">
            <span v-if="saving" class="spinner"></span>
            保存设置
          </button>
          <span v-if="saving" class="muted" style="font-size:12px">保存中...</span>
        </div>

      </div>
    </div>

    <!-- Proxy usage hint -->
    <div class="card" style="margin-top:14px">
      <div class="card-head">使用提示</div>
      <div class="card-body" style="padding:16px 18px">
        <div class="hint-grid">
          <div class="hint-item">
            <div class="hint-label">SOCKS5</div>
            <code>127.0.0.1:{{ form.socks5Port }}</code>
          </div>
          <div class="hint-item">
            <div class="hint-label">HTTP</div>
            <code>127.0.0.1:{{ form.httpPort }}</code>
          </div>
          <div class="hint-item" v-if="form.proxyMode !== 'socks5'">
            <div class="hint-label">透明代理</div>
            <code>:{{ form.tproxyPort }}</code>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.hint-grid { display: flex; gap: 20px; flex-wrap: wrap; }
.hint-item { display: flex; flex-direction: column; gap: 4px; }
.hint-label { font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: .1em; color: var(--muted); }
.hint-item code {
  font-family: var(--mono); font-size: 13px; color: var(--accent);
  background: var(--accent-bg); border: 1px solid var(--accent-glow);
  padding: 4px 10px; border-radius: 6px;
}
</style>
