#!/bin/bash

export name="skupper-van-form"
export namespace=""
export path="skupper"
export van=""
export url="http://127.0.0.1:8200"
export secret="skupper-van-form"
declare -A zones
export zones

declare -a published_zones
declare -a consumed_zones
export published_zones
export consumed_zones

usage() {
    echo "Generates a VanForm ConfigMap for a given site"
    echo
    echo "Use: $0 [-h|--help] [--path] [--van] [--zone] [-n|--namespace] [--name]"
    echo
    echo "    --path string            The default base path within Vault where tokens will be stored (default: skupper)"
    echo ""
    echo "    --van string             VAN name used to compose the path in Vault"
    echo ""
    echo "    --url string             Vault's URL (default: http://127.0.0.1:8200)"
    echo ""
    echo "    --secret string          Kubernetes secret name with Vault's credentials (default: skupper-van-form)"
    echo ""
    echo "    --zone strings           Defines a zone where your site is placed (you can enter it multiple times)"
    echo "                             Examples:"
    echo ""
    echo "                             # Placed at south zone (will consume tokens available to the south zone)"
    echo "                             --zone south"
    echo ""
    echo "                             # Placed at north zone (will consume tokens available to the north zone and"
    echo "                             # publish tokens to west, east and south zones)"
    echo "                             --zone north:west,east,south"
    echo ""
    echo "    --name string            Name of the generated ConfigMap (default: skupper-van-form)"
    echo "    -n|--namespace string    Name of the generated ConfigMap (default: skupper-van-form)"
    echo ""
    echo "    -h|--help                Show usage and exit"
    echo
    [ $# -gt 0 ] && echo "$@"
    exit 1
}

required_arg() {
    arg=$1
    if [[ "${arg}" =~ "=" ]]; then
      par="${arg%%=*}"
      val="${arg#*=}"
      [[ -z "${val}" ]] && usage "'${par}' is required"
    else
        [[ "$#" -lt 2 || -z "$2" ]] && usage "'${1}' is required"
    fi
}

required_value() {
    arg=$1
    if [[ "${arg}" =~ "=" ]]; then
      echo "${arg#*=}"
    else
      echo "${2}"
    fi
}

parse_args() {
    while [ $# -ne 0 ]; do
        case "${1}" in
            --path|--path=*)
                required_arg "$@"
                path=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            --van|--van=*)
                required_arg "$@"
                van=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            --url|--url=*)
                required_arg "$@"
                url=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            --secret|--secret=*)
                required_arg "$@"
                secret=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            --zone|--zone=*)
                required_arg "$@"
                zone_val=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                zone="${zone_val}"
                reachable_from=""
                if [[ "$zone_val" =~ ":" ]]; then
                    zone="${zone_val%%:*}"
                    reachable_from="${zone_val#*:}"
                fi
                zones[${zone}]=${reachable_from}
                ;;
            --name|--name=*)
                required_arg "$@"
                name=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            -n|--namespace|--namepace=*)
                required_arg "$@"
                namespace=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            -h|--help)
                usage
                ;;
            *)
                usage
                ;;
        esac
    done
    if [ ${#zones[@]} -eq 0 ]; then
        usage "At least one zone is required"
    fi
    required_arg "--van" "${van}"
    required_arg "--path" "${van}"
    required_arg "--name" "${name}"
}

consume_policy_def() {
    local zone="${1}"
    if contains_element "${zone}" "${consumed_zones[@]}"; then
        return
    fi
    consumed_zones+=("${zone}")
    cat << EOF
path "${path}/${van}/${zone}/links/*" {
  capabilities = ["read", "list"]
}
EOF
}

publish_policy_def() {
    local zone="${1}"
    if contains_element "${zone}" "${published_zones[@]}"; then
        return
    fi
    published_zones+=("${zone}")
    cat << EOF
path "${path}/${van}/${zone}/links/*" {
  capabilities = ["create", "update", "delete", "list"]
}
EOF
}

contains_element() {
    local element=$1
    shift
    local arr=("$@")
    for i in "${arr[@]}"; do
        [[ "${i}" = "${element}" ]] && return 0
    done
    return 1
}

config_json() {
    cat << EOF
{
    "van": "${van}",
    "url": "${url}",
    "path": "${path}",
    "secret": "${secret}",
EOF
    printf '    "zones": ['
    i=0
    for zone in "${!zones[@]}"; do
        IFS="," read -ra reachable_from_zones <<< "${zones[${zone}]}"
        reachable_from_def=""
        if [ ${#reachable_from_zones[@]} -gt 0 ]; then
            for reachable_from_zone in "${reachable_from_zones[@]}"; do
                [[ "${reachable_from_zone}" != "${reachable_from_zones[0]}" ]] && reachable_from_def+=", "
                reachable_from_def+=$(printf '"%s"' "${reachable_from_zone}")
            done
        fi
        if [ ${i} -gt 0 ]; then
            printf ", "
        fi
        printf '{"name": "%s", "reachable_from": [%s]}' "${zone}" "${reachable_from_def}"
        i=$((i+1))
    done
    printf ']\n}\n'
}

main() {
    parse_args "$@"

    kubectl --namespace "${namespace}" create configmap "${name}" \
        --dry-run=client \
        --output=yaml \
        --from-literal=config.json="$(config_json | jq)"
}

main "$@"
