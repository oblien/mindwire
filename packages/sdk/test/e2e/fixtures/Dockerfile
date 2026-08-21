# The image contract for mindwire's container layer, made concrete.
#
# `ensureDaemon` deploys the daemon into a container with a `bash -lc` script that uses curl (health
# probe), pkill (procps), setsid (util-linux), nohup, uname, mkdir/mv/chmod (coreutils). A bare
# debian:slim has bash + coreutils + util-linux but NOT curl or procps — so a coding-agent image must
# add them. This fixture documents that contract and is what container-docker.e2e.test.ts provisions.
#
# Build:  docker build -t mindwire-e2e:base packages/sdk/test/e2e/fixtures
FROM debian:stable-slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
       curl \
       procps \
       ca-certificates \
  && rm -rf /var/lib/apt/lists/*
