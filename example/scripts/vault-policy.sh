#!/bin/bash

export name="skupper"
export path="skupper"
export van=""
declare -A zones
export zones

declare -a published_zones
declare -a consumed_zones
export published_zones
export consumed_zones

usage() {
    echo "Generates a Vault policy that specifies access needed by a given VanForm region"
    echo
    echo "Use: $0 [-h|--help] [--path] [--van] [--zone] [--name]"
    echo
    echo "    --path string     The default base path within Vault where tokens will be stored (default: skupper)"
    echo ""
    echo "    --van string      VAN name used to compose the path in Vault"
    echo ""
    echo "    --zone strings    Defines a zone where your site is placed (you can enter it multiple times)"
    echo "                      Examples:"
    echo ""
    echo "                      # Placed at south zone (will consume tokens available to the south zone)"
    echo "                      --zone south"
    echo ""
    echo "                      # Placed at north zone (will consume tokens available to the north zone and"
    echo "                      # publish tokens to west, east and south zones)"
    echo "                      --zone north:west,east,south"
    echo ""
    echo "    --name string     Name of the generated Vault policy (default: skupper)"
    echo ""
    echo "    -h|--help       Show usage and exit"
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
            --name|--name=*)
                required_arg "$@"
                name=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
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
path "${path}/metadata/${van}/${zone}/links/*" {
  capabilities = ["read", "list"]
}
path "${path}/data/${van}/${zone}/links/*" {
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
  capabilities = ["create", "update", "delete", "read", "list"]
}
path "${path}/metadata/${van}/${zone}/links/*" {
  capabilities = ["create", "update", "delete", "read", "list"]
}
path "${path}/data/${van}/${zone}/links/*" {
  capabilities = ["create", "update", "delete", "read", "list"]
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

main() {
    parse_args "$@"
    for zone in "${!zones[@]}"; do
        IFS="," read -ra reachable_from_zones <<< "${zones[${zone}]}"
        consume_policy_def "${zone}"
        for reachable_from in "${reachable_from_zones[@]}"; do
            publish_policy_def "${reachable_from}"
        done
    done
}

main "$@"
