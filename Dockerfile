FROM eclipse-temurin:21-jdk-jammy
RUN apt-get update && apt-get install -y --no-install-recommends curl unzip ca-certificates && rm -rf /var/lib/apt/lists/*
ARG JADX=1.5.0
ARG APKTOOL=2.10.0
RUN curl -fsSL -o /tmp/jadx.zip https://github.com/skylot/jadx/releases/download/v${JADX}/jadx-${JADX}.zip \
 && unzip -q /tmp/jadx.zip -d /opt/jadx && rm /tmp/jadx.zip \
 && curl -fsSL -o /opt/apktool.jar https://github.com/iBotPeaches/Apktool/releases/download/v${APKTOOL}/apktool_${APKTOOL}.jar
ENV PATH="/opt/jadx/bin:${PATH}"
COPY decompile.sh /decompile.sh
ENTRYPOINT ["bash","/decompile.sh"]
