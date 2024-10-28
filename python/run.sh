#!/usr/bin/env bash

# bash safe mode. look at `set --help` to see what these are doing
set -euxo pipefail 

cd $(dirname $0)

# Be sure to use `exec` so that termination signals reach the python process,
# or handle forwarding termination signals manually
exec module/module $@
