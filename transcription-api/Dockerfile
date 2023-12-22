FROM anibali/pytorch:2.0.1-cuda11.8
USER root
RUN mkdir /var/log/whishper/
WORKDIR /app

ENV PYTHON_VERSION=3.10
ENV NVIDIA_VISIBLE_DEVICES all
ENV NVIDIA_DRIVER_CAPABILITIES compute,utility

RUN export DEBIAN_FRONTEND=noninteractive \
    && apt-get -qq update \
    && apt-get -qq install --no-install-recommends \
    python${PYTHON_VERSION} \
    python${PYTHON_VERSION}-venv \
    python3-pip \
    ffmpeg \
    curl wget xz-utils \
    ffmpeg nginx supervisor git \
    && rm -rf /var/lib/apt/lists/*

RUN ln -s -f /usr/bin/python${PYTHON_VERSION} /usr/bin/python3 && \
    ln -s -f /usr/bin/python${PYTHON_VERSION} /usr/bin/python && \
    ln -s -f /usr/bin/pip3 /usr/bin/pip

COPY requirements.txt requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip3 install -r requirements.txt
RUN --mount=type=cache,target=/root/.cache/pip pip3 install python-multipart

COPY . .

HEALTHCHECK --interval=30s --timeout=10s --start-period=15s \
CMD ["python3", "healthcheck.py"]

EXPOSE 8000
CMD ["python3", "main.py"]