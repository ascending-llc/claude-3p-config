# Cloud-key signing tool; see docs/codex-setup-guide.md.
FROM eclipse-temurin:21-jre@sha256:0b99b3376fbb50dbf9c0af8535fdd5b8eb0d3fdfc8b1e4967f383b6919f6901a

ARG JSIGN_VERSION=7.5
ARG JSIGN_SHA256=602a51c3545a6dc4fb99bd2ea7152b26d1345916d0c93ddfbd5936cb735af91c

RUN curl -fsSL -o /opt/jsign.jar "https://github.com/ebourg/jsign/releases/download/${JSIGN_VERSION}/jsign-${JSIGN_VERSION}.jar" \
 && echo "${JSIGN_SHA256}  /opt/jsign.jar" | sha256sum -c -

ENTRYPOINT ["java", "-jar", "/opt/jsign.jar"]
