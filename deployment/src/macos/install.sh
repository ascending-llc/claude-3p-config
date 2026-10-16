#!/bin/bash
# ASCENDING Claude Desktop macOS employee installer.
# Installs only the credential program, config.json, and named AWS profile.
# It does not sign in or install system policies.
set -euo pipefail
umask 077

profile_name="ASCENDING-CCWB"
aws_region="__BEDROCK_REGION__"
package_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
employee_home="${HOME:-}"
binary_written="no"
config_written="no"
aws_written="no"
backup_file=""
temp_config=""

fail() {
    printf 'Error: %s\n' "$1" >&2
    exit 1
}

cleanup() {
    exit_code=$?
    if [[ -n "$temp_config" && -f "$temp_config" ]]; then
        rm -f -- "$temp_config"
    fi
    if [[ "$exit_code" -ne 0 ]]; then
        printf 'Installation incomplete. Added in this run: credential program=%s, config.json=%s, AWS profile=%s.\n' \
            "$binary_written" "$config_written" "$aws_written" >&2
        if [[ -n "$backup_file" ]]; then
            printf 'Original AWS configuration backup: %s\n' "$backup_file" >&2
        fi
        printf 'Review the paths and backup above. Do not delete other AWS profiles or the entire ~/.aws directory.\n' >&2
    fi
}
trap cleanup EXIT

[[ "$(uname -s)" == "Darwin" ]] || fail 'This installer supports macOS only.'
[[ "$EUID" -ne 0 ]] || fail 'Run as the employee, without sudo.'
case "$(uname -m)" in
    arm64) binary_name="credential-process-darwin-arm64" ;;
    x86_64) binary_name="credential-process-darwin-amd64" ;;
    *) fail 'Unsupported Mac CPU architecture; only arm64 and x86_64 are supported.' ;;
esac

[[ "$employee_home" == /* && -d "$employee_home" ]] || fail 'HOME must be an absolute path to the current user directory.'
case "$employee_home" in
    *'"'*|*'$'*|*'`'*|*'\'*|*$'\n'*|*$'\r'*)
        fail 'HOME contains characters that cannot be safely written to the AWS credential_process command.'
        ;;
esac
[[ -z "${AWS_CONFIG_FILE+x}" && -z "${AWS_SHARED_CREDENTIALS_FILE+x}" ]] || \
    fail 'AWS_CONFIG_FILE or AWS_SHARED_CREDENTIALS_FILE is set; confirm the custom AWS configuration location first.'
[[ -w "$employee_home" ]] || fail "The current user directory is not writable: $employee_home"

source_binary="$package_dir/$binary_name"
source_config="$package_dir/config.json"
for input_file in "$source_binary" "$source_config"; do
    [[ -f "$input_file" && -s "$input_file" && -r "$input_file" ]] || \
        fail "The bundle is missing a readable, nonempty file: $input_file"
done

install_dir="$employee_home/claude-code-with-bedrock"
target_binary="$install_dir/credential-process"
target_config="$install_dir/config.json"
aws_dir="$employee_home/.aws"
aws_config="$aws_dir/config"
aws_credentials="$aws_dir/credentials"
expected_command="\"$target_binary\" --profile $profile_name"

for target_dir in "$install_dir" "$aws_dir"; do
    [[ ! -L "$target_dir" ]] || fail "Directory is a symlink; refusing to write: $target_dir"
    [[ ! -e "$target_dir" || -d "$target_dir" ]] || fail "Target is not a directory: $target_dir"
    if [[ -e "$target_dir" ]]; then
        [[ -w "$target_dir" ]] || fail "Directory is not writable: $target_dir"
    fi
done

check_existing_file() {
    local source_file="$1"
    local target_file="$2"
    [[ ! -L "$target_file" ]] || fail "Target is a symlink; refusing to overwrite: $target_file"
    if [[ -e "$target_file" ]]; then
        [[ -f "$target_file" && -r "$target_file" ]] || fail "Target is not a readable regular file: $target_file"
        cmp -s "$source_file" "$target_file" || fail "Existing file differs; review it manually: $target_file"
    fi
}
check_existing_file "$source_binary" "$target_binary"
check_existing_file "$source_config" "$target_config"

[[ ! -L "$aws_config" ]] || fail "AWS configuration is a symlink; refusing to modify: $aws_config"
[[ ! -e "$aws_config" || ( -f "$aws_config" && -r "$aws_config" ) ]] || \
    fail "AWS configuration is not a readable regular file: $aws_config"
[[ ! -L "$aws_credentials" ]] || fail "AWS credentials file is a symlink; review it manually: $aws_credentials"
if [[ -e "$aws_credentials" ]]; then
    [[ -f "$aws_credentials" && -r "$aws_credentials" ]] || \
        fail "AWS credentials file is not a readable regular file: $aws_credentials"
    credential_state="$(awk -v name="$profile_name" '
        /^[[:space:]]*\[[^]]+\]/ {
            section = $0
            sub(/^[[:space:]]*\[/, "", section)
            sub(/\].*$/, "", section)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", section)
            if (section == name || section == "profile " name) found = 1
        }
        END { print found ? "conflict" : "clear" }
    ' "$aws_credentials")" || fail "Cannot read AWS credentials file: $aws_credentials"
    [[ "$credential_state" != "conflict" ]] || \
        fail "AWS credentials file contains a section with the same name; review it manually: $aws_credentials"
fi

profile_state="missing"
if [[ -e "$aws_config" ]]; then
    profile_state="$(awk -v name="profile $profile_name" \
        -v want_region="$aws_region" -v want_command="$expected_command" '
        function trim(value) {
            sub(/^[[:space:]]+/, "", value)
            sub(/[[:space:]]+$/, "", value)
            return value
        }
        /^[[:space:]]*\[[^]]+\]/ {
            section = $0
            sub(/^[[:space:]]*\[/, "", section)
            sub(/\].*$/, "", section)
            active = (trim(section) == name)
            if (active) count++
            next
        }
        active {
            if ($0 ~ /^[[:space:]]*($|[#;])/) next
            separator = index($0, "=")
            if (!separator) { unexpected = 1; next }
            key = tolower(trim(substr($0, 1, separator - 1)))
            value = trim(substr($0, separator + 1))
            if (key == "region") {
                region_count++
                region_value = value
            } else if (key == "credential_process") {
                command_count++
                command_value = value
            } else {
                unexpected = 1
            }
        }
        END {
            if (!count) print "missing"
            else if (count == 1 && region_count == 1 && command_count == 1 &&
                     region_value == want_region && command_value == want_command &&
                     !unexpected) print "same"
            else print "conflict"
        }
    ' "$aws_config")" || fail "Cannot read AWS configuration: $aws_config"
fi
[[ "$profile_state" != "conflict" ]] || \
    fail "AWS configuration already has a different [profile $profile_name]; it will not be overwritten: $aws_config"

# Write only after all conflict checks. System configuration profiles and old
# policies are outside this script's scope.
mkdir -p -- "$install_dir" "$aws_dir"
if [[ ! -e "$target_binary" ]]; then
    install -m 700 "$source_binary" "$target_binary"
    binary_written="yes"
fi
if [[ ! -e "$target_config" ]]; then
    install -m 600 "$source_config" "$target_config"
    config_written="yes"
fi
chmod 600 "$target_config"

if [[ "$profile_state" == "missing" ]]; then
    if [[ -e "$aws_config" ]]; then
        backup_timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
        backup_candidate="$(mktemp "$aws_dir/config.ascending-ccwb.backup.$backup_timestamp.XXXXXX")"
        if ! cp -p "$aws_config" "$backup_candidate"; then
            rm -f -- "$backup_candidate"
            fail "Cannot back up AWS configuration: $aws_config"
        fi
        backup_file="$backup_candidate"
    fi
    temp_config="$(mktemp "$aws_dir/.config.ascending-ccwb.XXXXXX")"
    if [[ -e "$aws_config" ]]; then
        cp -p "$aws_config" "$temp_config"
        printf '\n' >> "$temp_config"
    fi
    printf '[profile %s]\nregion = %s\ncredential_process = %s\n' \
        "$profile_name" "$aws_region" "$expected_command" >> "$temp_config"
    mv -f -- "$temp_config" "$aws_config"
    temp_config=""
    aws_written="yes"
fi

printf 'Local files are ready: %s, %s\n' "$target_binary" "$target_config"
printf 'AWS profile is ready: %s (%s)\n' "$profile_name" "$aws_config"
printf 'Added in this run: credential program=%s, config.json=%s, AWS profile=%s.\n' \
    "$binary_written" "$config_written" "$aws_written"
if [[ -n "$backup_file" ]]; then
    printf 'Original AWS configuration backup: %s\n' "$backup_file"
fi
printf 'Next, run this yourself: "%s" --login --profile %s\n' "$target_binary" "$profile_name"
printf 'Then install Claude-CCWB.mobileconfig in macOS settings and reopen Claude Desktop.\n'
