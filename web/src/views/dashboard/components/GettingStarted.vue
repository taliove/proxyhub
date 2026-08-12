<template>
  <!-- 首日引导(critique P2):机场数为 0 = 还没接入任何上游,此时四张统计卡全是 0、
       异常面板无内容,产品价值完全隐身;用三步引导把「第一步」亮出来。
       loaded 把关,加载期与接口失败(保留空态)均不闪现。 -->
  <el-card v-if="showGuide" class="getting-started" shadow="never">
    <div class="gs-head">
      <div class="gs-title">一个链接全设备通用，三步跑通</div>
      <div class="gs-sub">ProxyHub 把多个机场聚合成一个稳定订阅地址，客户端只认这一个链接。</div>
    </div>
    <div class="gs-steps">
      <div v-for="s in steps" :key="s.num" class="gs-step">
        <span class="gs-num num">{{ s.num }}</span>
        <div class="gs-step-body">
          <div class="gs-step-title">{{ s.title }}</div>
          <div class="gs-step-desc">{{ s.desc }}</div>
        </div>
      </div>
    </div>
    <div class="gs-actions">
      <el-button type="primary" @click="router.push({ name: 'Airports' })">添加机场</el-button>
      <el-button @click="router.push({ name: 'Endpoints' })">新建订阅地址</el-button>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useDashboardStats } from '../composables/useDashboardStats'

const router = useRouter()
const { stats, loaded } = useDashboardStats()

const showGuide = computed(() => loaded.value && stats.value.airports === 0)

const steps = [
  { num: 1, title: '添加机场', desc: '接入上游订阅 URL，或直接粘贴导出内容建手动机场' },
  { num: 2, title: '刷新入池', desc: '系统拉取节点并健康检查，问题节点在上桌前被拦下' },
  { num: 3, title: '分发订阅地址', desc: '每台设备一个链接，节点优劣由系统替你操心' }
]
</script>

<style scoped>
.getting-started {
  border-radius: var(--ph-radius-lg);
}
.gs-head {
  margin-bottom: var(--ph-space-4);
}
.gs-title {
  font-size: var(--ph-text-lg);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.gs-sub {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.gs-steps {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: var(--ph-space-4);
  margin-bottom: var(--ph-space-4);
}
@media (max-width: 768px) {
  .gs-steps {
    grid-template-columns: 1fr;
  }
}
.gs-step {
  display: flex;
  gap: var(--ph-space-3);
  padding: var(--ph-space-3);
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius);
}
.gs-num {
  flex-shrink: 0;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--ph-radius-full);
  background: var(--ph-bg-hover);
  color: var(--ph-primary);
  font-weight: 600;
  font-size: var(--ph-text-sm);
}
.gs-step-title {
  font-weight: 600;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-primary);
}
.gs-step-desc {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
}
</style>
