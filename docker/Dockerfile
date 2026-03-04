ARG NODE_VERSION=24
FROM node:${NODE_VERSION}-alpine AS node
FROM lscr.io/linuxserver/firefox:latest AS production

COPY --from=node /usr/lib /usr/lib
COPY --from=node /usr/local/lib /usr/local/lib
COPY --from=node /usr/local/include /usr/local/include
COPY --from=node /usr/local/bin /usr/local/bin

RUN node -v

FROM production AS dev

RUN apk add \
  git \
  quilt \
  python3 \
  alpine-sdk \
  libx11-dev \
  libxkbfile-dev \
  libstdc++ \
  libc6-compat \
  libsecret-dev \
  jq \
  rsync
