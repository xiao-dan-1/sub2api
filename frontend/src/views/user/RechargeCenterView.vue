<template>
  <AppLayout>
    <section class="recharge-center" data-testid="recharge-center">
      <div class="recharge-center-shell">
        <a
          :href="storeUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-secondary btn-sm recharge-center-external"
          data-testid="recharge-center-external-link"
          :aria-label="t('purchase.openInNewTab')"
          :title="t('purchase.openInNewTab')"
        >
          <Icon name="externalLink" size="sm" :stroke-width="2" />
          <span class="hidden sm:inline">{{ t('purchase.openInNewTab') }}</span>
        </a>

        <iframe
          :src="storeUrl"
          :title="t('nav.rechargeCenter')"
          class="recharge-center-frame"
          allow="clipboard-write; payment"
        ></iframe>
      </div>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const storeUrl = 'https://catfk.com/shop/BD9COW6C'
</script>

<style scoped>
.recharge-center {
  height: calc(100vh - 6rem);
  height: calc(100dvh - 6rem);
  min-height: 36rem;
}

.recharge-center-shell {
  @apply relative h-full w-full overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm;
  @apply dark:border-dark-700 dark:bg-dark-900;
}

.recharge-center-external {
  @apply absolute right-3 top-3 z-10 gap-1.5 shadow-sm;
  @apply backdrop-blur supports-[backdrop-filter]:bg-white/90;
  @apply dark:supports-[backdrop-filter]:bg-dark-800/90;
}

.recharge-center-frame {
  @apply block h-full w-full border-0 bg-white;
}

@media (min-width: 768px) {
  .recharge-center {
    height: calc(100vh - 7rem);
    height: calc(100dvh - 7rem);
  }
}

@media (min-width: 1024px) {
  .recharge-center {
    height: calc(100vh - 8rem);
    height: calc(100dvh - 8rem);
  }
}
</style>
