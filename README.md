# WhalePet 鲸鱼娘桌宠 🐋

一个用 **纯 Go**（零第三方依赖、无 CGO）编写的 Windows 桌宠应用，内置 **mem reduct** 同款内存清理引擎。桌宠形象来自 `DSniang1.png` 的鲸鱼娘。

## ✨ 功能

| 功能 | 说明 |
|---|---|
| 🐋 透明置顶桌宠 | 无边框、背景完全透明的鲸鱼娘，漂浮 + 呼吸 + 摇摆动画 |
| 💬 气泡对话 | 头顶气泡实时显示内存占用（绿/黄/红三色），还会随机聊天 |
| 🧹 一键清理 | **左键点击宠物**立即清理内存，显示释放了多少 |
| ✨ 清理动画 | 清理时摇晃 + 蓝色星光粒子 + 转圈点点，完成后雀跃 + 金色星星 |
| ⏱ 自动清理 | 内存使用率达到阈值（70%/80%/90%）自动清理，带 30 秒冷却 |
| � 清理方式 | 标准清理（memreduct 默认掩码）/ 深度清理（额外清空待机列表） |
| 📌 托盘图标 | 托盘左键清理，右键打开菜单，悬停显示内存占用 |
| 💾 记忆位置 | 关闭时保存宠物位置和设置（`%AppData%\WhalePet\config.txt`） |

## 🛠 构建

需要安装 [Go 1.21+](https://go.dev/dl/)（Windows，amd64）：

```powershell
cd DesktopPet
go build -ldflags "-s -w -H windowsgui" -o WhalePet.exe .
```

* `-H windowsgui`：隐藏控制台窗口（桌宠必备）
* 首次运行会触发一次 UAC（内存清理需要 `SeProfileSingleProcessPrivilege` 权限）；
  拒绝 UAC 也能运行，但清理功能不可用（可从右键菜单重新以管理员身份启动）。

## 🎮 使用方法

| 操作 | 效果 |
|---|---|
| 左键点击宠物 | 立即清理内存 |
| 按住左键拖动 | 移动宠物位置（自动限制在屏幕内） |
| 右键宠物 / 托盘图标 | 打开菜单（立即清理 / 自动清理 / 清理方式 / 关于 / 退出） |
| 左键点击托盘图标 | 立即清理内存 |

## 🧠 清理引擎（移植自 [mem reduct](https://github.com/henrypp/memreduct)）

与 memreduct 相同，通过 `NtSetSystemInformation` 执行清理：

| 掩码位 | 操作 | 系统调用 |
|---|---|---|
| ✅ 标准 | 工作集修剪 | `SystemMemoryListInformation / MemoryEmptyWorkingSets` |
| ✅ 标准 | 系统文件缓存修剪 | `SystemFileCacheInformationEx`（Min/Max = SIZE_MAX） |
| ✅ 标准 | 优先级-0 待机列表清空 | `SystemMemoryListInformation / MemoryPurgeLowPriorityStandbyList` |
| ✅ 标准 | 卷缓存刷新 | `FindFirstVolumeW` + `FlushFileBuffers` |
| ✅ 标准 | 注册表缓存刷新 | `SystemRegistryReconciliationInformation`（Win8.1+） |
| ✅ 标准 | 物理页合并 | `SystemCombinePhysicalMemoryInformation`（Win10+） |
| ⚠️ 深度 | 待机列表全清 | `SystemMemoryListInformation / MemoryPurgeStandbyList` |
| ⚠️ 深度 | 已修改页列表刷新 | `SystemMemoryListInformation / MemoryFlushModifiedList` |

> ⚠️ **深度清理**会清空全部待机列表，可能造成短暂系统卡顿（与 memreduct 的"freezes"警告一致），故默认关闭，请从菜单按需开启。

启动时会启用 `SeProfileSingleProcessPrivilege` 与 `SeIncreaseQuotaPrivilege` 权限（与 memreduct 一致）。释放量统计口径也与 memreduct 相同：清理前后「已用物理内存」之差。

## 📁 项目结构

```
DesktopPet/
├── DSniang1.png        # 桌宠形象（构建时嵌入 exe）
├── go.mod
├── main.go             # 入口：单实例、UAC 提权、GDI+ 初始化
├── win32.go            # Win32 API 声明（类型 / 常量 / 过程）
├── gdiplus.go          # GDI+ 绑定（PNG 解码 / 绘制 / 文本 / 变换）
├── cleaner.go          # 内存清理引擎（memreduct 移植）
├── app.go              # 桌宠窗口、托盘、菜单、消息循环
├── render.go           # 动画与渲染（粒子、气泡）
└── config.go           # 配置持久化
```

## ❓ 常见问题

- **为什么启动时要管理员权限？** 待机列表清空等操作需要 `SeProfileSingleProcessPrivilege`，该权限仅管理员可用——这一点与 memreduct 完全一致。
- **双击 exe 没反应？** 请检查任务栏托盘区，鲸鱼娘默认出现在屏幕右下角任务栏上方。
- **图片不透明怎么办？** 程序会自动检测：若源图完全不透明，会自动应用圆角羽化遮罩，保证桌宠效果。
