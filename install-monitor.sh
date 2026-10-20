#!/usr/bin/env bash
# Upgrade an existing, standard systemd installation of 3x-ui v2.8.8.
set -Eeuo pipefail
umask 077

SOURCE_COMMIT='146e2a8c9e1b427ac95cf425cf73a1b8da4e5b2f'
GO_VERSION='1.25.6'
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
        x86_64|amd64) go_arch=amd64; download_arch=amd64; go_arm='' ;;
        aarch64|arm64) go_arch=arm64; download_arch=arm64; go_arm='' ;;
        i386|i486|i586|i686) go_arch=386; download_arch=386; go_arm='' ;;
        armv7*) go_arch=arm; download_arch=armv6l; go_arm=7 ;;
        armv6*) go_arch=arm; download_arch=armv6l; go_arm=6 ;;
        s390x) go_arch=s390x; download_arch=s390x; go_arm='' ;;
        *) die "不支持的架构：$(uname -m)" ;;
    esac
}

install_dependencies() {
    local missing=0 tool
    for tool in curl tar gzip gcc make python3 sha256sum flock install; do
        command -v "$tool" >/dev/null 2>&1 || missing=1
    done
    [[ $missing == 1 ]] || return 0
    log '安装编译和下载依赖……'
    if command -v apt-get >/dev/null 2>&1; then
        apt-get update
        DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl tar gzip build-essential python3 util-linux coreutils
    elif command -v dnf >/dev/null 2>&1; then
        dnf install -y ca-certificates curl tar gzip gcc make glibc-devel python3 util-linux coreutils
    elif command -v yum >/dev/null 2>&1; then
        yum install -y ca-certificates curl tar gzip gcc make glibc-devel python3 util-linux coreutils
    elif command -v pacman >/dev/null 2>&1; then
        pacman -S --needed --noconfirm ca-certificates curl tar gzip gcc make glibc python util-linux coreutils
    elif command -v zypper >/dev/null 2>&1; then
        zypper --non-interactive install ca-certificates curl tar gzip gcc make glibc-devel python3 util-linux coreutils
    else
        die '未找到支持的包管理器，请安装 curl、tar、gzip、gcc、make、Python 3、sha256sum、flock 后重试。'
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
    if [[ $work_dir == /tmp/x-ui-monitor.* && -d $work_dir ]]; then
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
        grep -Eq '^[[:space:]]*(export[[:space:]]+)?XUI_DB_FOLDER=' /etc/default/x-ui "$INSTALL_DIR/.env" 2>/dev/null; then
        die '检测到数据库目录环境配置，请按文档手动安装并备份实际数据库。'
    fi
    [[ -f "$DATA_DIR/x-ui.db" ]] || die '未找到默认数据库 /etc/x-ui/x-ui.db，请确认安装路径。'
}

build_monitor() {
    local filename checksum
    log "下载经过验证的监控源码：$SOURCE_COMMIT"
    fetch "https://codeload.github.com/Joan-bbs/3x-ui/tar.gz/$SOURCE_COMMIT" "$work_dir/source.tar.gz"
    mkdir "$work_dir/source"
    tar -xzf "$work_dir/source.tar.gz" --strip-components=1 -C "$work_dir/source"
    [[ -f "$work_dir/source/web/html/inbound_monitor.html" ]] || die '源码缺少入站监控页面。'

    log "下载临时 Go $GO_VERSION 工具链，不修改系统 Go……"
    filename="go${GO_VERSION}.linux-${download_arch}.tar.gz"
    fetch 'https://go.dev/dl/?mode=json&include=all' "$work_dir/go-versions.json"
    checksum=$(python3 - "$work_dir/go-versions.json" "$GO_VERSION" "$filename" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as stream:
    releases = json.load(stream)
for release in releases:
    if release["version"] == "go" + sys.argv[2]:
        for item in release["files"]:
            if item["filename"] == sys.argv[3]:
                print(item["sha256"])
                sys.exit(0)
sys.exit("官方 Go 下载列表中没有匹配的工具链")
PY
    )
    [[ $checksum =~ ^[0-9a-f]{64}$ ]] || die 'Go 工具链校验信息无效。'
    fetch "https://go.dev/dl/$filename" "$work_dir/go.tar.gz"
    verify_sha "$work_dir/go.tar.gz" "$checksum" || die 'Go 工具链 SHA256 校验失败。'
    tar -xzf "$work_dir/go.tar.gz" -C "$work_dir"
    log '编译监控版，首次下载依赖可能需要几分钟；此时原面板继续运行。'
    (
        cd "$work_dir/source"
        export GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=1 CC=gcc
        export GOOS=linux GOARCH="$go_arch" GOARM="$go_arm" GOFLAGS=''
        export GOPATH="$work_dir/gopath" GOCACHE="$work_dir/cache" GOMAXPROCS=2
        "$work_dir/go/bin/go" test -p 2 ./xray ./web/monitor
        "$work_dir/go/bin/go" build -p 2 -trimpath -ldflags '-s -w' -o "$work_dir/x-ui-monitor" main.go
    )
    [[ $("$work_dir/x-ui-monitor" -v) == '2.8.8' ]] || die '新程序版本检查失败。'
}

replace_binary() {
    local i restart_count
    log '编译成功，短暂停服并备份程序、配置和数据库……'
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
    systemctl start "$SERVICE"
    for ((i = 0; i < 10; i++)); do
        sleep 1
        systemctl is-active --quiet "$SERVICE" || die '新程序启动检查失败。'
        [[ $(systemctl show "$SERVICE" -p NRestarts --value) == "$restart_count" ]] || die '检测到新程序反复重启。'
    done
    finished=1
    log '安装完成！登录原面板，在「入站列表」下方打开「入站监控」。'
    log '原账号、入站、证书、面板端口和访问路径保持不变。'
}

main() {
    [[ $EUID == 0 ]] || die '请使用 root 用户运行本脚本。'
    [[ $(uname -s) == Linux ]] || die '本脚本仅支持 Linux。'
    command -v systemctl >/dev/null 2>&1 || die '本脚本适用于 systemd 安装，不支持 Docker 或 OpenRC。'
    check_installation
    detect_arch
    install_dependencies
    exec 9>/run/lock/x-ui-monitor.lock
    flock -n 9 || die '已有另一个入站监控安装任务正在运行。'
    # Check again after obtaining the installation lock.
    check_installation
    work_dir=$(mktemp -d /tmp/x-ui-monitor.XXXXXX)
    trap on_exit EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    build_monitor
    replace_binary
}

if [[ ${BASH_SOURCE[0]} == "$0" ]]; then
    main "$@"
fi
