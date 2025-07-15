#!/bin/bash

export name="skupper-van-form"
export namespace=""
export role_id=""
export secret_id=""

usage() {
    echo "Generates a Kubernetes secret to be used by VanForm"
    echo
    echo "Use: $0 [-h|--help] [-n|--namespace] [--name] role-id secret-id"
    echo
    echo "    role-id         The role-id used to authenticate against Vault (required)"
    echo "    secret-id       The secret-id used to authenticate against Vault (required)"
    echo "    -n|--namespace  Secret's namespace"
    echo "    --name          Secret's name (default: skupper-van-form)"
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
            -n|--namespace|--namespace=*)
                required_arg "$@"
                namespace=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            --name|--name=*)
                required_arg "$@"
                name=$(required_value "$@")
                # shellcheck disable=SC2015
                [[ "$1" =~ "=" ]] && shift || shift 2
                ;;
            -h|--help)
                usage
                ;;
            *)
                [[ -n "${role_id}" && -n "${secret_id}" ]] && usage "Invalid argument $1"
                if [[ -z "${role_id}" ]]; then
                    required_arg "role-id" "$1"
                    role_id="$1"
                    shift
                else
                    required_arg "secret-id" "$1"
                    secret_id="$1"
                    shift
                fi
                ;;
        esac
    done
    [[ -z "${role_id}" ]] && usage "role-id is required"
    [[ -z "${secret_id}" ]] && usage "secret-id is required"
}

main() {
    parse_args "$@"

    kubectl --namespace "${namespace}" create secret generic "${name}" \
        --dry-run=client \
        --output=yaml \
        --from-literal=role-id="${role_id}" \
        --from-literal=secret-id="${secret_id}"
}

main "$@"
