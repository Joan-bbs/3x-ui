# 入站端口实时速度监控（v2.8.8）

基准：官方 v2.8.8，提交 `d8c783a2968926634dc29d5a8149815c4623ed16`。

登录面板后，在「入站列表」与「面板设置」之间选择「入站监控」。页面每 2 秒更新，显示当前账号各入站的端口、备注、监听地址、协议、开关状态、上传速度和下载速度，并支持搜索、速度排序和暂停。手机端优先显示端口及双向速度，其余列可横向滑动查看。沿用项目侧栏、Ant Design Vue 组件及浅色、深色、超深色主题。

上传和下载以客户端为准，使用 1024 进制的字节/秒。顶部合计包含当前账号全部入站，不随搜索结果改变。未收到有效采样、连接失败或 Xray 停止时显示「—」，没有流量时显示 `0 B/s`。暂停及后台标签页停止请求，恢复后重新获取数据。

监控读取 Xray 入站计数，兼容原有每 10 秒清零的额度采集；监控不会额外清零计数或改变额度结算频率。采样和原有清零操作串行执行，清零量用于校正采样基线。Xray 重启、查询失败或超过 10 秒未采样后重新建立基线，首次进入通常需要等待一次刷新。

接口为登录鉴权下的 `GET /panel/api/inbounds/speeds`（支持自定义面板根路径），只返回当前账号的入站元数据及速度，不返回客户端配置。Xray 的 `policy.system.statsInboundUplink` 和 `statsInboundDownlink` 需启用；v2.8.8 默认配置已启用。此功能统计 Xray 逻辑入站流量，不包含网卡的协议开销或其他进程流量。

## 一键安装（服务器无需编译）

本仓库 Actions 的 **Release Inbound Monitor** 会在 GitHub 服务器构建 Linux amd64、ARM64 静态程序并发布到 Releases。首个版本为 `v2.8.8-monitor.1`；**必须等该工作流两个架构均构建成功、Release 附件出现后再运行安装命令**。

root 用户执行：

```bash
bash <(curl -fsSL "https://raw.githubusercontent.com/Joan-bbs/3x-ui/feat/v2.8.8-inbound-speed-monitor/install-monitor.sh")
```

已有标准 systemd v2.8.8 安装：下载监控程序和 SHA256SUMS，校验后备份原目录及数据库，只替换面板程序。无需 Go、GCC 或在 VPS 上编译，临时下载保存在磁盘 `/var/tmp`，避开小容量 `/tmp` 内存盘。升级保留 Xray、账号、入站、证书、端口和面板路径；启动失败会尝试恢复原程序，备份位于 `/var/backups/x-ui-monitor/`。原服务停止时保持停止。

全新安装：先下载并验证监控包，再自动调用固定版本的官方 v2.8.8 安装脚本部署基础环境（按官方提示设置账号/证书），最后替换监控程序。全新安装会下载官方基础包和监控包，均不需要编译。只支持 systemd、标准路径 `/usr/local/x-ui`、默认数据库 `/etc/x-ui`；其他版本、自定义路径、Docker/OpenRC 会拒绝。

手动发布：Actions → Release Inbound Monitor → Run workflow，分支选择 `feat/v2.8.8-inbound-speed-monitor`，填写未使用的新标签（如 `v2.8.8-monitor.2`）。发布工作流不会覆盖旧版本。安装指定新版本：

```bash
curl -fLsS "https://raw.githubusercontent.com/Joan-bbs/3x-ui/feat/v2.8.8-inbound-speed-monitor/install-monitor.sh" -o /root/install-monitor.sh
MONITOR_VERSION=v2.8.8-monitor.2 bash /root/install-monitor.sh
```

工作流首次修改推送到此分支时自动触发首次构建；如果 fork 的 Actions 未启用，先在 Actions 页面启用，再手动运行。默认安装版本固定为 `v2.8.8-monitor.1`，发布新版后应显式指定版本或更新脚本默认值。不要通过官方更新菜单覆盖监控版。

发布包尚不存在、下载失败或校验失败会在停服前退出，不会退回 VPS 编译。安装完成仍需登录面板确认真实代理流量。

原源码编译安装器保留为 `install-monitor-source.sh`（仍需服务器编译，临时目录已改为 `/var/tmp`）。下面仅供需要自行构建的用户：

在与服务器架构匹配的 Linux 环境中，准备 Go 1.25.6 或更高版本、Git、GCC 后执行：

```bash
git clone --single-branch --branch feat/v2.8.8-inbound-speed-monitor https://github.com/Joan-bbs/3x-ui.git
cd 3x-ui
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o x-ui main.go
```

备份数据库和原程序，停止面板服务后，用编译产物替换安装目录中的 `x-ui` 可执行文件，再启动服务。常规安装位置为 `/usr/local/x-ui/x-ui`；以实际服务配置为准。保留原数据库、Xray 程序和配置。本次没有修改数据库结构、发布版本或官方安装脚本。

## 验证

```bash
go test ./xray ./web/monitor ./web/service ./web/controller ./web
node --test web/assets/js/inbound-monitor.test.cjs
bash -n install-monitor.sh
bash tests/install-monitor.test.sh
CGO_ENABLED=1 go build ./...
```

Go 测试覆盖跨多次额度清零计算速度、空闲计数、计数回退、新入站预热和长间隔后的重新采样。前端测试覆盖搜索与合计、请求去重、暂停时取消请求、忽略过期响应、后台停止/恢复及错误重试。

上线后可用单个入站传输文件检查速度变化；跨过 10 秒额度采集周期应保持连续，停止传输后速度应回到零。停止/重启 Xray 后应显示状态或重新采样，流量总量仍由原额度任务记录。
