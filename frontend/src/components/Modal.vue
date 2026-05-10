<script setup>
defineProps({
  title:   { type: String, default: '' },
  small:   { type: Boolean, default: false },
  large:   { type: Boolean, default: false },
  noClose: { type: Boolean, default: false },
})
const emit = defineEmits(['close'])
</script>

<template>
  <Teleport to="body">
    <div class="overlay" @mousedown.self="!noClose && emit('close')">
      <div :class="['modal', small && 'modal-sm', large && 'modal-lg']">
        <div class="modal-head">
          <slot name="title">{{ title }}</slot>
          <button v-if="!noClose" class="close-btn" @click="emit('close')">×</button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
        <div v-if="$slots.foot" class="modal-foot">
          <slot name="foot" />
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed; inset: 0; background: rgba(0,0,0,.7);
  display: flex; align-items: center; justify-content: center;
  z-index: 1000; backdrop-filter: blur(5px);
  animation: fadeIn .16s ease;
}
@keyframes fadeIn { from { opacity:0; } to { opacity:1; } }

.modal { animation: slideUp .18s ease; }
@keyframes slideUp {
  from { opacity: 0; transform: translateY(10px); }
  to   { opacity: 1; transform: translateY(0); }
}
</style>
