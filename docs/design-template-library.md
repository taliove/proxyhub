# 模板库 - 设计

## 概述

配置模板从"每用户一份"升级为**用户级模板库**:每用户可建多套命名模板(其中一套为默认),订阅地址创建/编辑时从库中挑选一套;不挑则沿回退链下落。动机与权衡见 ADR 0030(演进 ADR 0006)。

## 模型

**template 表**(库):`(id, user_id, name, content, is_default, created_at, updated_at)`,`UNIQUE(user_id, name)`,每用户至多一行 `is_default=1`(SetDefault 在事务内先清后设)。

**endpoints.template_name**:订阅地址对库成员的**软引用**(字符串,空串 = 跟随默认)。无外键、无级联:名称 miss 即回退,删除被引用模板不需要改写引用方。

**user_quotas.max_templates**:创建模板的数量配额(默认 10,超管在用户管理中调整)。

**迁移**(022,Go 内联 `migrateTemplateLibrary`):重建 template 表带出 is_default 与唯一约束,存量 `name='clash'` 行标默认。守卫 = "template 表是否已有 is_default 列"(PRAGMA 探测),整体单事务。守卫不能按中间表 `template_library` 是否存在判定——该表在迁移末尾被重命名,据此判定会让每次重启重跑重建、把自定义默认重置为 0(回归测试 `TestTemplateLibraryMigrationPreservesDefault`)。

## 关键流程

**渲染时模板解析(四级回退)**,落点 `resolveTemplateForEndpoint`:

```
endpoint.template_name 非空且库中命中 → 用之        (Level 1,软引用 miss 即下落)
?? 用户默认模板 (is_default=1)                      (Level 2)
?? 超管全局默认 (system_settings.clash_template)     (Level 3)
?? 内嵌默认 (generator.DefaultTemplate,永不失败)      (Level 4)
```

每级 miss/空继续下落,渲染永远有产出。仅 Clash 格式走模板;V2Ray(base64 链接列表)不涉及。

**写入侧校验(fail-fast)**:创建/更新订阅地址带非空 `template_name` 时,必须存在于该用户库中,否则 400(`ErrNotFound` 经 `errors.Is` 映射)。

**删除语义**:允许删除被引用模板;DELETE 响应带 `ref_count`(引用该模板的订阅地址数),前端在确认框前置提示"N 个订阅地址将改用默认模板"。

**多用户语义**:库接口全部按 EffectiveUserID 过滤;普通用户只能见/操作自己的库(跨用户按名访问 = 404);超管 impersonate 时操作目标用户库,未 impersonate 时维持超管全局默认编辑面(`/api/settings/template`,不走库)。

## 与其他模块的边界

- 与**租户级设置**(CONTEXT.md)同构:回退链是同一套"用户覆盖 ?? 全局默认 ?? 内置默认"范式在模板域的实例,只是用户覆盖从单行变成了库。
- 与 **conditions**(design-subscription-conditions)正交:conditions 决定"下发哪些节点",模板决定"配置骨架长什么样";两者在订阅地址上独立配置。
- 与**名称标准化**(ADR 0012)同为订阅地址级覆盖,`name_mode/name_template` 是先例,`template_name` 沿用同模式(端点列 + 空串跟随)。
