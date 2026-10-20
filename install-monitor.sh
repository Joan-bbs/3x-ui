#!/usr/bin/env bash
# Install or upgrade 3x-ui v2.8.8 monitor from verified release binaries.
set -Eeuo pipefail
umask 077

RELEASE_REPO='Joan-bbs/3x-ui'
MONITOR_VERSION=${MONITOR_VERSION:-v2.8.8-monitor.2}
INSTALL_DIR='/usr/local/x-ui'
DATA_DIR='/etc/x-ui'
BACKUP_ROOT='/var/backups/x-ui-monitor'
SERVICE='x-ui.service'
work_dir=''
backup_dir=''
stopped=0
replaced=0
was_active=0
finished=0

log() { printf '\n[入站监控] %s\n' "$*"; }
die() { printf '\n[错误] %s\n' "$*" >&2; exit 1; }

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) download_arch=amd64 ;;
        aarch64|arm64) download_arch=arm64 ;;
        *) die "没有此架构的预编译包：$(uname -m)。支持 x86_64、ARM64。" ;;
    esac
}

install_dependencies() {
    local missing=0 tool
    for tool in curl tar gzip sha256sum flock install ip tc; do
        command -v "$tool" >/dev/null 2>&1 || missing=1
    done
    [[ $missing == 1 ]] || return 0
    log '安装下载和校验工具（无需编译器）……'
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl tar gzip util-linux coreutils iproute2
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y ca-certificates curl tar gzip util-linux coreutils iproute
    elif command -v yum >/dev/null 2>&1; then
        yum install -y ca-certificates curl tar gzip util-linux coreutils iproute
    elif command -v pacman >/dev/null 2>&1; then
        pacman -S --needed --noconfirm ca-certificates curl tar gzip util-linux coreutils iproute2
    elif command -v zypper >/dev/null 2>&1; then
        zypper --non-interactive install ca-certificates curl tar gzip util-linux coreutils iproute2
    else
        die '未找到支持的包管理器，请安装 curl、tar、gzip、sha256sum、flock、ip、tc 后重试。'
    fi
}

fetch() {
    curl --fail --location --show-error --silent --retry 3 --connect-timeout 20 --max-time 900 "$1" -o "$2"
}

verify_sha() {
    printf '%s  %s\n' "$2" "$1" | sha256sum --check --status
}

on_exit() {
    local status=$?
    trap - EXIT INT TERM
    set +e
    if [[ $finished == 0 && $stopped == 1 ]]; then
        log '安装未完成，恢复原有程序和服务状态……'
        systemctl stop "$SERVICE"
        if [[ $replaced == 1 ]]; then
            if install -m 755 "$backup_dir/program/x-ui" "$INSTALL_DIR/x-ui.monitor-rollback" &&
                mv -f -- "$INSTALL_DIR/x-ui.monitor-rollback" "$INSTALL_DIR/x-ui"; then
                log '原程序已恢复。'
            else
                printf '自动恢复失败，请使用备份：%s\n' "$backup_dir" >&2
                was_active=0
            fi
        fi
        if [[ $was_active == 1 ]]; then
            systemctl start "$SERVICE" || printf '原服务启动失败，请检查 systemctl status x-ui。\n' >&2
        fi
    fi
    if [[ -n $backup_dir ]]; then
        printf '\n备份目录：%s\n' "$backup_dir"
    fi
    # Only remove the private temporary directory created by this script.
    if [[ $work_dir == /var/tmp/x-ui-monitor.* && -d $work_dir ]]; then
        rm -rf -- "$work_dir"
    fi
    exit "$status"
}

check_installation() {
    [[ -x "$INSTALL_DIR/x-ui" ]] || die '未找到 /usr/local/x-ui/x-ui。请先安装官方 v2.8.8，再运行本脚本。'
    local version exec_start
    version=$("$INSTALL_DIR/x-ui" -v)
    [[ $version == '2.8.8' || $version == 'v2.8.8' ]] || die "当前版本为 $version；本脚本只升级 v2.8.8，避免降级数据库。"
    [[ $(systemctl show "$SERVICE" -p LoadState --value) == loaded ]] || die '未找到已安装的 x-ui systemd 服务。'
    exec_start=$(systemctl show "$SERVICE" -p ExecStart --value)
    [[ $exec_start == *"path=$INSTALL_DIR/x-ui ;"* ]] || die '服务使用了自定义程序路径，请按文档手动安装。'
    # Custom database locations need an explicit backup plan; do not guess them.
    if [[ -n ${XUI_DB_FOLDER:-} && ${XUI_DB_FOLDER} != "$DATA_DIR" ]]; then
        die '检测到自定义 XUI_DB_FOLDER，请按文档手动安装并备份实际数据库。'
    fi
    if systemctl show "$SERVICE" -p Environment --value | grep -q 'XUI_DB_FOLDER=' ||
        grep -Eq '^[[:space:]]*(export[[:space:]]+)?XUI_DB_FOLDER[[:space:]]*=' \
            /etc/default/x-ui /etc/sysconfig/x-ui /etc/conf.d/x-ui "$INSTALL_DIR/.env" 2>/dev/null; then
        die '检测到数据库目录环境配置，请按文档手动安装并备份实际数据库。'
    fi
    [[ -f "$DATA_DIR/x-ui.db" ]] || die '未找到默认数据库 /etc/x-ui/x-ui.db，请确认安装路径。'
}

download_monitor() {
    local filename checksum
    [[ $MONITOR_VERSION =~ ^v2\.8\.8-monitor\.[0-9]+$ ]] || die '无效的监控版发布版本。'
    filename="x-ui-linux-${download_arch}.tar.gz"
    local url="https://github.com/$RELEASE_REPO/releases/download/$MONITOR_VERSION"
    log "下载监控版 $MONITOR_VERSION（$download_arch），服务器无需编译……"
    fetch "$url/$filename" "$work_dir/$filename" || die "安装包尚未发布或下载失败。请先在 GitHub Actions 运行 Release Inbound Monitor，发布 $MONITOR_VERSION。"
    fetch "$url/SHA256SUMS" "$work_dir/SHA256SUMS" || die '下载校验文件失败。'
    checksum=$(awk -v name="$filename" '$2 == name && length($1) == 64 {print $1}' "$work_dir/SHA256SUMS")
    [[ $checksum =~ ^[0-9a-f]{64}$ ]] || die '缺少有效的安装包校验信息。'
    verify_sha "$work_dir/$filename" "$checksum" || die '安装包 SHA256 校验失败。'
    # Extract only the known executable: no archive-provided paths or symlinks.
    tar -xOzf "$work_dir/$filename" x-ui/x-ui > "$work_dir/x-ui-monitor"
    chmod 755 "$work_dir/x-ui-monitor"
    [[ $("$work_dir/x-ui-monitor" -v) == '2.8.8' ]] || die '新程序版本检查失败。'
}

bootstrap_official() {
    [[ ! -e "$DATA_DIR/x-ui.db" && ! -e "$INSTALL_DIR" ]] || die '发现残留安装或数据库，请先确认原安装，避免覆盖。'
    log '全新安装：先安装官方 v2.8.8 基础环境，随后替换为监控版。'
    fetch 'https://raw.githubusercontent.com/mhsanaei/3x-ui/v2.8.8/install.sh' "$work_dir/official-install.sh"
    bash "$work_dir/official-install.sh" v2.8.8
}

replace_binary() {
    local i restart_count
    log '安装包已验证，短暂停服并备份程序、配置和数据库……'
    backup_dir="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)-$$"
    mkdir -p "$backup_dir/program"
    if systemctl is-active --quiet "$SERVICE"; then was_active=1; fi
    stopped=1
    systemctl stop "$SERVICE"
    cp -a -- "$INSTALL_DIR/." "$backup_dir/program/"
    cp -a -- "$DATA_DIR" "$backup_dir/data"
    systemctl cat "$SERVICE" > "$backup_dir/service.txt"
    # Staging and rename are on the same filesystem; never truncate a live binary.
    install -m 755 "$work_dir/x-ui-monitor" "$INSTALL_DIR/x-ui.monitor-new"
    replaced=1
    mv -f -- "$INSTALL_DIR/x-ui.monitor-new" "$INSTALL_DIR/x-ui"
    restart_count=$(systemctl show "$SERVICE" -p NRestarts --value)
    if [[ $was_active == 1 ]]; then
    systemctl start "$SERVICE"
    for ((i = 0; i < 10; i++)); do
        sleep 1
        systemctl is-active --quiet "$SERVICE" || die '新程序启动检查失败。'
        [[ $(systemctl show "$SERVICE" -p NRestarts --value) == "$restart_count" ]] || die '检测到新程序反复重启。'
    done
    fi
    finished=1
    log '安装完成！登录原面板，在侧栏打开「入站监控」或其下方的「端口限速」。'
    log '原账号、入站、证书、面板端口和访问路径保持不变。'
}

main() {
    [[ $EUID == 0 ]] || die '请使用 root 用户运行本脚本。'
    [[ $(uname -s) == Linux ]] || die '本脚本仅支持 Linux。'
    command -v systemctl >/dev/null 2>&1 || die '本脚本适用于 systemd 安装，不支持 Docker 或 OpenRC。'
    if [[ -x "$INSTALL_DIR/x-ui" ]]; then check_installation; fi
    detect_arch
    install_dependencies
    exec 9>/run/lock/x-ui-monitor.lock
    flock -n 9 || die '已有另一个入站监控安装任务正在运行。'
    # Check again after obtaining the installation lock.
    if [[ -x "$INSTALL_DIR/x-ui" ]]; then check_installation; fi
    work_dir=$(mktemp -d /var/tmp/x-ui-monitor.XXXXXX)
    trap on_exit EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    download_monitor
    if [[ ! -x "$INSTALL_DIR/x-ui" ]]; then bootstrap_official; fi
    check_installation
    replace_binary
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    main "$@"
fi
