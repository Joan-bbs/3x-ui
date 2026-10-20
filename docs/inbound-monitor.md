# 入站端口实时速度监控（v2.8.8）

基准：官方 v2.8.8，提交 `d8c783a2968926634dc29d5a8149815c4623ed16`。

登录面板后，在「入站列表」与「面板设置」之间选择「入站监控」。页面每 2 秒更新，显示当前账号各入站的端口、备注、监听地址、协议、开关状态、上传速度和下载速度，并支持搜索、速度排序和暂停。手机端优先显示端口及双向速度，其余列可横向滑动查看。沿用项目侧栏、Ant Design Vue 组件及浅色、深色、超深色主题。

上传和下载以客户端为准，使用 1024 进制的字节/秒。顶部合计包含当前账号全部入站，不随搜索结果改变。未收到有效采样、连接失败或 Xray 停止时显示「—」，没有流量时显示 `0 B/s`。暂停及后台标签页停止请求，恢复后重新获取数据。

监控读取 Xray 入站计数，兼容原有每 10 秒清零的额度采集；监控不会额外清零计数或改变额度结算频率。采样和原有清零操作串行执行，清零量用于校正采样基线。Xray 重启、查询失败或超过 10 秒未采样后重新建立基线，首次进入通常需要等待一次刷新。

接口为登录鉴权下的 `GET /panel/api/inbounds/speeds`（支持自定义面板根路径），只返回当前账号的入站元数据及速度，不返回客户端配置。Xray 的 `policy.system.statsInboundUplink` 和 `statsInboundDownlink` 需启用；v2.8.8 默认配置已启用。此功能统计 Xray 逻辑入站流量，不包含网卡的协议开销或其他进程流量。

## 一键安装（服务器无需编译）

本仓库 Actions 的 **Release Inbound Monitor** 会在 GitHub 服务器构建 Linux amd64、ARM64 静态程序并发布到 Releases。首个版本为 `v2.8.8-monitor.2`；**必须等该工作流两个架构均构建成功、Release 附件出现后再运行安装命令**。

root 用户执行：

```bash
bash <(curl -fsSL "https://raw.githubusercontent.com/Joan-bbs/3x-ui/feat/v2.8.8-inbound-speed-monitor/install-monitor.sh")
```

已有标准 systemd v2.8.8 安装：下载监控程序和 SHA256SUMS，校验后备份原目录及数据库，只替换面板程序。无需 Go、GCC 或在 VPS 上编译，临时下载保存在磁盘 `/var/tmp`，避开小容量 `/tmp` 内存盘。升级保留 Xray、账号、入站、证书、端口和面板路径；启动失败会尝试恢复原程序，备份位于 `/var/backups/x-ui-monitor/`。原服务停止时保持停止。

全新安装：先下载并验证监控包，再自动调用固定版本的官方 v2.8.8 安装脚本部署基础环境（按官方提示设置账号/证书），最后替换监控程序。全新安装会下载官方基础包和监控包，均不需要编译。只支持 systemd、标准路径 `/usr/local/x-ui`、默认数据库 `/etc/x-ui`；其他版本、自定义路径、Docker/OpenRC 会拒绝。

手动发布：Actions → Release Inbound Monitor → Run workflow，分支选择 `feat/v2.8.8-inbound-speed-monitor`，填写未使用的新标签（如 `v2.8.8-monitor.3`）。发布工作流不会覆盖旧版本。安装指定新版本：

```bash
curl -fLsS "https://raw.githubusercontent.com/Joan-bbs/3x-ui/feat/v2.8.8-inbound-speed-monitor/install-monitor.sh" -o /root/install-monitor.sh
MONITOR_VERSION=v2.8.8-monitor.2 bash /root/install-monitor.sh
```

工作流首次修改推送到此分支时自动触发首次构建；如果 fork 的 Actions 未启用，先在 Actions 页面启用，再手动运行。默认安装版本固定为 `v2.8.8-monitor.2`，发布新版后应显式指定版本或更新脚本默认值。不要通过官方更新菜单覆盖监控版。

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

## 端口限速

在「入站监控」下方新增「端口限速」，选择承载流量的网卡后，分别设置客户端上传和下载上限，单位为十进制 Mbps，整数 1–100000，0 表示不限速。支持修改和取消。每个端口的 TCP/UDP、IPv4/IPv6、所有连接共用该方向的一个限速桶。限速作用于所选网卡上的整个端口；即使绑定了不同监听地址，相同端口也共用上限。

采用 Linux tc clsact + flower + 共享 police 动作，超过桶容量的流量丢包，存在短时突发，TCP 实际吞吐可能低于设置值；这是速率上限，不保证精确恒速。上传在 ingress 匹配目标端口，下载在 egress 匹配源端口。规则匹配实际经过网卡的数据包；反代/隧道内部逻辑端口不一定直接出现在物理网卡包头上，应按实际部署选择端口和网卡。

一键脚本安装 iproute2/iproute，不在 VPS 编译。面板需要 root 或 CAP_NET_ADMIN，内核需要 clsact、flower、police 支持；不满足时设置会报错，不会假报成功。最多 128 个端口。使用 49152/49153 过滤器优先级、带专用 cookie 的动作编号，不替换根队列，不删除其他程序的动作或过滤器；已有 ingress 队列时拒绝覆盖。若有其他流控策略，它们也会影响实际速率。

配置写入数据库目录中的 `port-limits.json`（仅 root 可读写），面板启动时恢复。请使用稳定网卡名；恢复失败会在页面显示错误，可重新保存规则重试。入站删除/改端口后旧规则会保留并标为「旧端口规则」，可直接取消。卸载或切换回官方版本之前先取消全部限速，否则内核规则会持续到重启/人工清理。

接口仅允许操作当前账号入站，跨账号共享端口拒绝配置，面板自身端口禁止限速。设置变更失败时尝试恢复旧规则；恢复错误会一并返回。
