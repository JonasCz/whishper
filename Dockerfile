# YT-DLP Download and setup
FROM --platform=$BUILDPLATFORM golang:bookworm AS ytdlp_cache
ARG TARGETOS
ARG TARGETARCH
RUN apt update && apt install -y wget
RUN wget https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -O /usr/local/bin/yt-dlp
RUN chmod a+rx /usr/local/bin/yt-dlp

# Backend setup
FROM devopsworks/golang-upx:latest as backend-builder

ENV DEBIAN_FRONTEND noninteractive
WORKDIR /app
COPY ./backend /app
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o whishper . && \
    upx whishper
RUN chmod a+rx whishper

# Frontend setup
FROM node:alpine as frontend
ENV PNPM_HOME="/pnpm"
ENV PATH="$PNPM_HOME:$PATH"
RUN corepack enable
COPY ./frontend /app
WORKDIR /app

FROM frontend AS frontend-prod-deps
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --prod --frozen-lockfile

FROM frontend AS frontend-build
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
ENV BODY_SIZE_LIMIT=0
RUN pnpm run build

# Base container
FROM anibali/pytorch:2.0.1-cuda11.8 as base
USER root

ENV PYTHON_VERSION=3.10

RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get -qq update \
    && apt-get -qq install --no-install-recommends \
    python${PYTHON_VERSION} \
    python${PYTHON_VERSION}-venv \
    python3-pip \
    ffmpeg \
    curl wget xz-utils \
    ffmpeg nginx supervisor git wget \
    && wget --quiet --show-progress https://github.com/Run-Pod/runpodctl/releases/download/v1.10.0/runpodctl-linux-amd -O runpodctl && chmod +x runpodctl && sudo cp runpodctl /usr/bin/runpodctl \
    && rm -rf /var/lib/apt/lists/*

#Runpod setup
COPY ./.runpod.yaml /home/user/.runpod.yaml

RUN ln -s -f /usr/bin/python${PYTHON_VERSION} /usr/bin/python3 && \
    ln -s -f /usr/bin/python${PYTHON_VERSION} /usr/bin/python && \
    ln -s -f /usr/bin/pip3 /usr/bin/pip

# Python service setup
COPY ./transcription-api /app/transcription
WORKDIR /app/transcription
RUN pip install --no-cache-dir -r requirements.txt && \
    pip install --no-cache-dir python-multipart
 
# Node.js service setup
RUN wget https://nodejs.org/dist/v20.9.0/node-v20.9.0-linux-x64.tar.xz && \
    tar -xf node-v20.9.0-linux-x64.tar.xz && \
    mv node-v20.9.0-linux-x64 /usr/local/lib/node && \
    rm node-v20.9.0-linux-x64.tar.xz
ENV PATH="/usr/local/lib/node/bin:${PATH}"

ENV BODY_SIZE_LIMIT=0
COPY ./frontend /app/frontend
COPY --from=frontend-build /app/build /app/frontend
COPY --from=frontend-prod-deps /app/node_modules /app/frontend/node_modules

# Golang service setup
COPY --from=backend-builder /app/whishper /bin/whishper 
RUN chmod a+rx /bin/whishper
COPY --from=ytdlp_cache /usr/local/bin/yt-dlp /bin/yt-dlp

# Nginx setup
COPY ./nginx.conf /etc/nginx/nginx.conf
COPY ./.htpasswd /etc/nginx/.htpasswd

# Set workdir and entrypoint
WORKDIR /app
RUN mkdir /app/uploads

# Cleanup to make the image smaller
RUN apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/* /usr/share/doc/* ~/.cache /var/cache

RUN mkdir -p /var/log/whishper/

COPY ./supervisord.conf /etc/supervisor/conf.d/supervisord.conf
ENTRYPOINT ["supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"]

# Expose ports for each service and Nginx
EXPOSE 8080 3000 5000 80