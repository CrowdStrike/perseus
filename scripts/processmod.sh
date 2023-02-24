#! /bin/bash -e

function cleanup {
    # restore Git branch and PWD if needed
    if [ -n "${_PERSEUS_INITIAL_BRANCH}"  -a "${_PERSEUS_INITIAL_BRANCH}" != "$(git rev-parse --abbrev-ref HEAD)" ]; then
        echo "Resetting Git branch to ${_PERSEUS_INITIAL_BRANCH}"
        git checkout -q ${_PERSEUS_INITIAL_BRANCH}
    fi
    if [ -n "${_PERSEUS_START_DIR}" ]; then
        cd ${_PERSEUS_START_DIR}
    fi
}
trap cleanup EXIT

_INCLUDE_PRERELEASE=
_POSITIONAL_ARGS=()
while [[ $# -gt 0 ]]
do
    arg="$1"
    case $arg in
    -p|--prerelease)
        _INCLUDE_PRERELEASE=--prerelease
        shift; ## past flag
        ;;
    -s|--server)
        PERSEUS_SERVER_ADDR=$2
        if [ -z $PERSEUS_SERVER_ADDR ]; then
            echo The Perseus server address \(host:port\) is required after -s/--server.
            exit 1
        fi
        shift # past flag
        shift # past value
        ;;
    *)
        _POSITIONAL_ARGS+=("$1")
        shift
        ;;
    esac
done
set -- "${_POSITIONAL_ARGS[@]}"

if [ $# -gt 1 ]; then
    echo Only 1 positional argument supported
    exit 1
fi

# use explicit module path at $1, if specified.  otherwise default to PWD.
_PERSEUS_MODULE_PATH=$1
if [ -n "${_PERSEUS_MODULE_PATH}" ] && [ "${_PERSEUS_MODULE_PATH}" != "${PWD}" ]; then
    _PERSEUS_START_DIR=${PWD}
    cd ${_PERSEUS_MODULE_PATH}
else 
    _PERSEUS_MODULE_PATH=${PWD}
fi

# grab initial branch so that we can switch back when we're done
_PERSEUS_INITIAL_BRANCH=$(git rev-parse --abbrev-ref HEAD)

# set Perseus server location if unset
if [ -z "${PERSEUS_SERVER_ADDR}" ]; then
    echo "defaulting to Perseus server at localhost:31138"
    export PERSEUS_SERVER_ADDR=localhost:31138
fi

# let's go
echo "Processing module at ${_PERSEUS_MODULE_PATH}"
for t in `git tag --list 'v*' --sort=-v:refname`; do
    echo "   analyzing ${t} ..."
    git checkout -q ${t}
    perseus update --path ${_PERSEUS_MODULE_PATH} --version ${t} ${_INCLUDE_PRERELEASE}
done
