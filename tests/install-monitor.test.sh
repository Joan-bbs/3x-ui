#!/usr/bin/env bash
# Runs entirely against temporary files and mocked systemctl; no root required.
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../install-monitor.sh"

test_root=$(mktemp -d "${TMPDIR:-/tmp}/x-ui-installer-test.XXXXXX")
trap 'rm -rf -- "$test_root"' EXIT

setup_case() {
    INSTALL_DIR="$test_root/$1/install"
    DATA_DIR="$test_root/$1/data"
    BACKUP_ROOT="$test_root/$1/backups"
    work_dir="$test_root/$1/work"
    backup_dir=''
    stopped=0; replaced=0; was_active=0; finished=0
    mkdir -p "$INSTALL_DIR" "$DATA_DIR" "$work_dir"
    printf 'original\n' > "$INSTALL_DIR/x-ui"
    printf 'new\n' > "$work_dir/x-ui-monitor"
    printf 'database-content\n' > "$DATA_DIR/x-ui.db"
    printf 'existing-settings\n' > "$INSTALL_DIR/.env"
}

systemctl() {
    printf '%s\n' "$*" >> "$INSTALL_DIR/service-calls"
    case "$1" in
        show) printf '0\n' ;;
        cat) printf 'unchanged service\n' ;;
        start)
            if [[ ${fail_start:-0} == 1 && $(cat "$INSTALL_DIR/x-ui") == new ]]; then
                return 1
            fi
            ;;
    esac
}
sleep() { :; }
install() {
    [[ $1 == -m && $2 == 755 ]]
    shift 2
    cp -- "$1" "$2"
}

# Successful replacement preserves configuration/data and creates an offline backup.
(
    setup_case success
    replace_binary
    [[ $(cat "$INSTALL_DIR/x-ui") == new ]]
    [[ $(cat "$INSTALL_DIR/.env") == existing-settings ]]
    [[ $(cat "$DATA_DIR/x-ui.db") == database-content ]]
    [[ $(cat "$backup_dir/program/x-ui") == original ]]
    cmp "$DATA_DIR/x-ui.db" "$backup_dir/data/x-ui.db"
    [[ $finished == 1 ]]
)
printf 'PASS: successful upgrade preserves configuration and backs up database\n'

# A failed start exits nonzero, restores the executable and starts the old service.
set +e
(
    set -e
    setup_case rollback
    fail_start=1
    trap on_exit EXIT
    replace_binary
)
result=$?
set -e
[[ $result != 0 ]]
[[ $(cat "$test_root/rollback/install/x-ui") == original ]]
[[ $(cat "$test_root/rollback/data/x-ui.db") == database-content ]]
[[ $(grep -c '^start ' "$test_root/rollback/install/service-calls") == 2 ]]
printf 'PASS: failed startup restores original binary and service\n'

# Validate architecture mapping.
(
    uname() { printf 'aarch64\n'; }
    detect_arch
    [[ $go_arch == arm64 && $download_arch == arm64 ]]
    uname() { printf 'armv7l\n'; }
    detect_arch
    [[ $go_arch == arm && $download_arch == armv6l && $go_arm == 7 ]]
)
printf 'PASS: architecture mapping\n'

if command -v sha256sum >/dev/null 2>&1; then
    checksum=$(sha256sum "$test_root/success/data/x-ui.db")
    verify_sha "$test_root/success/data/x-ui.db" "${checksum%% *}"
    if verify_sha "$test_root/success/data/x-ui.db" '0000000000000000000000000000000000000000000000000000000000000000'; then
        printf 'corrupt archive was accepted\n' >&2
        exit 1
    fi
    printf 'PASS: checksum verification rejects corrupted downloads\n'
else
    printf 'SKIP: checksum test requires sha256sum (not supplied in minimal Git Bash)\n'
fi
