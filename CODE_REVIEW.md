# WhalePet 代码审查报告

> 审查范围：`E:\DeepSeek\DesktopPet` 下的 Go 源码（`main.go` / `app.go` / `render.go` / `cleaner.go` / `config.go` / `debug.go` / `gdiplus.go` / `win32.go`）以及 `memreduct/` 参考子目录。
> 审查日期：2026-10-03 · 状态：`go build` 通过，`go vet` 存在 4 条 `unsafe.Pointer` 警告。

---

## 一、项目概览

**WhalePet（鲸鱼娘桌宠）** 是一个 Windows 桌面宠物，内置从开源项目 [mem reduct](https://github.com/henrypp/memreduct) 移植的内存清理引擎。

**技术栈**
- 纯 Go（`go.mod`：`go 1.21`，amd64），零第三方依赖、无 CGO。
- 通过 `syscall` + `NewLazyDLL` 直接绑定 Win32 / GDI+ / ntdll / advapi32 等原生 API。
- 使用 `//go:build windows` 构建标签，仅支持 Windows 且为 amd64（结构体按 x64 布局）。

**目录结构（Go 部分）**
| 文件 | 职责 |
|---|---|
| `main.go` | 入口：单实例锁、提升权限（UAC）、GDI+ 初始化、组装 App、主循环 |
| `app.go` | 透明置顶窗口、托盘图标、右键菜单、鼠标输入、定时器与消息循环 |
| `render.go` | 动画（漂浮/呼吸/摇摆/摇晃/雀跃）、粒子系统、对话气泡、`UpdateLayeredWindow` 呈现 |
| `cleaner.go` | 内存清理引擎（memreduct 移植），通过 `NtSetSystemInformation` 执行清理掩码 |
| `config.go` | 轻量持久化（`%AppData%\WhalePet\config.txt`） |
| `debug.go` | 临时诊断日志与测试开关（`dbg` / `--test-clean`） |
| `gdiplus.go` | 扁平化 GDI+ 绑定（PNG 解码、ARGB 绘制、抗锯齿、变换、文本） |
| `win32.go` | Win32 API 声明、结构体、常量、权限与令牌工具 |

> `memreduct/` 子目录为上游 C++ 项目的 vendored 副本，**不属于** Go 模块，`go build` 不会编译它。`cleaner.go` 是其清理逻辑的 Go 移植参考，本报告只做点到为止的说明，不逐行审查上游 C++ 源码。

---

## 二、架构与模块划分

```
main.go
  ├─ 单实例检测（FindWindowW "WhalePetMainWnd"）
  ├─ DPI 感知（SetProcessDPIAware）
  ├─ 提权判断（isElevated / tryElevate，--elevated 跳过）
  ├─ GDI+ 启动
  ├─ App 初始化 + 负载配置
  └─ a.run() 消息循环
        ├─ wndProc 处理 鼠标/托盘/定时器 消息
        ├─ onSecond()  （1s 定时器）：内存采样、托盘提示、自动清理判定、闲聊
        ├─ onTick()    （33ms 定时器）：动画帧 + 清理结果回调
        └─ requestClean() → goroutine → cleanMemory() → cleanDone 通道
```

- **清理** 在独立 goroutine 中执行，动画帧不被阻塞；完成后通过带缓冲的 `cleanDone` 通道回传，`onTick` 中来消费，避免卡顿。
- **资源管理**：`cleanup()` 逆序释放 GDI+ 对象 / DIB / DC / 图标 / 令牌，配合 `defer` 保证退出时释放。
- **UI 呈现**：32bpp 自上而下 DIB + `GdipCreateBitmapFromScan0` 的 PARGB 表面，经 `UpdateLayeredWindow` 做逐像素 alpha 合成。

---

## 三、优点

1. **零依赖、无 CGO**：用纯标准库 + `syscall` 完成所有 Win32/GDI+ 交互，分发与维护成本低。
2. **忠实移植**：`cleaner.go` 对 memreduct 的掩码、`SYSTEM_INFORMATION_CLASS` 值、`SYSTEM_MEMORY_LIST_COMMAND` 值做了对应，清理名称与注释清晰。
3. **职责清晰**：窗口/输入、渲染、清理、配置、绑定各自独立成文件，可读性较好。
4. **动画不卡帧**：清理在 goroutine 中执行，渲染由独立 33ms 定时器驱动。
5. **优雅降级**：非管理员运行时仍可运行（仅清理不可用），并提示可通过右键菜单重启为管理员。
6. **资源回收完备**：`cleanup()` 把可回收对象逐一清零，避免泄漏。
7. **单实例与位置记忆**：单实例检测 + 退出时保存窗口位置与设置。

---

## 四、问题与改进建议

### 4.1 未使用 / 死代码（详见第五节清单）
- `gpFillRect` 以及它唯一用到的 `procGdipFillRectangleI` 从未被调用。
- `procGetWindowRect`、`procGetSystemMetrics` 两个 proc 声明后从未使用。
- 常量 `WS_VISIBLE`、`SM_CXSCREEN`、`SM_CYSCREEN`、`ERROR_ALREADY_EXISTS` 无引用。
- `App.ramAvail`、`App.ramTotal` 两个字段「只写不读」，属于死字段。

### 4.2 `go vet` `checkptr` 警告（`gdiplus.go`）
`go vet` 输出 4 条 `possible misuse of unsafe.Pointer`，位于传入系统调用参数处（如 `uintptr(unsafe.Pointer(&stream))`、`uintptr(unsafe.Pointer(&bmp))`、`uintptr(unsafe.Pointer(&g))`、`uintptr(unsafe.Pointer(&bd))`）。
- 这些在 syscall 调用期间是常见写法，多属**误报**；但建议确保返回的 `uintptr` 不跨越调用存活。若想消除告警，可将这些 `&local` 的取地址写得让 `checkptr` 满意（如用 `unsafe.Pointer` 直接传入而非先转 `uintptr`），或在关注点处加局部变量 `uintptr` 而非直接传 `&x`。

### 4.3 魔法数字
- `app.go` 中 `exStyle := uintptr(WS_EX_LAYERED | WS_EX_TOPMOST | WS_EX_TOOLWINDOW | 0x08000000)` 里的 `0x08000000` 实为 `WS_EX_NOACTIVATE`，建议提取为命名常量以提升可读性。

### 4.4 错误处理偏弱
- 大量调用使用 `_, _, _` 忽略返回值，如 GDI+ 创建、`SetTimer`、`MoveWindow` 等，失败时难以察觉。建议对关键路径（如窗口/DIB/字体创建）校验返回值并给出更明确的错误信息。

### 4.5 调试代码残留
- `debug.go` 会在当前目录无条件生成 `whalepet-debug.log`，`dbg` 在 `drawBubble` 中每次绘制都写日志，可能产生大量写入。建议：仅在使用 `--test-clean` / 调试构建时启用，或用 `build tag` 隔离。
- `--test-clean` 测试钩子（`testClean`/`testFired`）属于测试代码，保留在生产路径中，建议仅在 `go build -tags debug` 下编译。

### 4.6 其它
- `math/rand` 依赖 Go 1.20+ 的自动 seeding（`go 1.21` 满足），闲聊/粒子颜色具备随机性，无需手动 `Seed`。
- 结构体按 amd64 布局硬编码（如 `NOTIFYICONDATAW` 984 字节），一旦跨架构（如 arm64）需重新核对；README 也已注明 amd64-only。
- `requestClean` 在非管理员时的提示与菜单中的“以管理员身份重启”体验尚可，但 `tryElevate` 失败时仅返回 false，缺少更细致的错误提示。
- `cleaner.go` 的 `flushVolumeCache` 设置为 `CreateFileW` 打开卷后立即 `FlushFileBuffers`，与 memreduct 行为一致；对无法打开卷静默忽略，符合“尽力而为”的设计。

---

## 五、未使用的接口 / 函数 / 变量清单（重点）

> 判定依据：对全部 Go 源码进行全量引用搜索，确认以下符号 **从未被调用/引用**。

### 5.1 未使用的函数

| 符号 | 位置 | 说明 |
|---|---|---|
| `gpFillRect` | `gdiplus.go:230` | 定义一个矩形填充函数，但项目内无任何调用点；渲染仅使用 `gpFillEllipse` / `gpFillRoundedRect` / `gpDrawRoundedRect` / `gpDrawLine` |
| `procGdipFillRectangleI` | `gdiplus.go:38` | 仅在 `gpFillRect` 内部引用，随 `gpFillRect` 一并成为死代码 |
| `procGetWindowRect` | `win32.go:233` | 声明后从未使用（获取窗口矩形未用到） |
| `procGetSystemMetrics` | `win32.go:237` | 声明后从未使用（工作区改用 `SystemParametersInfoW`） |

### 5.2 未使用的常量

| 常量 | 位置 | 说明 |
|---|---|---|
| `WS_VISIBLE` | `win32.go:37` | 窗口样式常量，未引用 |
| `SM_CXSCREEN` | `win32.go:55` | 屏幕尺寸索引，未引用 |
| `SM_CYSCREEN` | `win32.go:56` | 屏幕尺寸索引，未引用 |
| `ERROR_ALREADY_EXISTS` | `win32.go:95` | 错误码常量，未引用 |

### 5.3 未使用的结构体字段（死字段）

| 字段 | 位置 | 说明 |
|---|---|---|
| `App.ramAvail` | `app.go:89` | 在 `onSecond()`（`app.go:447`）被赋值，但全工程无任何读取点 |
| `App.ramTotal` | `app.go:90` | 在 `onSecond()`（`app.go:448`）被赋值，但全工程无任何读取点 |

> 注：Go 编译器不会把包级未使用函数/常量/字段判为错误（仅未使用的局部变量与导入会报错），因此上述符号虽不影响编译，但属于可清理的死代码，建议删除以降低维护噪音。

---

## 六、总结

WhalePet 总体实现质量较好：纯 Go、无依赖、架构清晰、资源回收完备，且对 memreduct 的清理语义移植准确。主要遗留问题集中在**少量死代码**（`gpFillRect` 等）与**忽略返回值带来的脆弱性**、**调试日志常开**等工程化细节上。建议优先清理第五节列出的未使用符号，并将 `--test-clean`/`dbg` 用构建标签隔离后再发布。
