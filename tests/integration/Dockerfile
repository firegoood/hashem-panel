FROM golang:1.27-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61
RUN apt-get update -qq && apt-get install -y -qq iproute2 iptables iputils-ping python3 shellcheck systemd ca-certificates curl && rm -rf /var/lib/apt/lists/*
RUN curl --proto '=https' --tlsv1.2 -fsSL https://github.com/fatedier/frp/releases/download/v0.71.0/frp_0.71.0_linux_amd64.tar.gz -o /tmp/frp.tgz \
    && echo '84f27e39f11169f7adcef8e8b70c9329de17747b1f14dad9fb95eef5682ea716  /tmp/frp.tgz' | sha256sum -c - \
    && tar -xzf /tmp/frp.tgz -C /tmp frp_0.71.0_linux_amd64/frpc frp_0.71.0_linux_amd64/frps \
    && mkdir -p /usr/local/lib/hashem/frp-0.71.0 \
    && install -m 755 /tmp/frp_0.71.0_linux_amd64/frpc /tmp/frp_0.71.0_linux_amd64/frps /usr/local/lib/hashem/frp-0.71.0/ \
    && install -m 755 /tmp/frp_0.71.0_linux_amd64/frpc /tmp/frp_0.71.0_linux_amd64/frps /usr/local/bin/ \
    && rm -rf /tmp/frp*
WORKDIR /work
