FROM jenkins/jenkins:lts-jdk17

USER root

ARG GO_VERSION=1.25.2
ARG TARGETARCH
ARG TARGETPLATFORM
ENV GO_ARCH=${TARGETARCH:-amd64}

RUN echo "Building for TARGETARCH=${TARGETARCH} TARGETPLATFORM=${TARGETPLATFORM}" \
  && uname -m

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl git make unzip docker.io \
  && rm -rf /var/lib/apt/lists/*

RUN git config --system --add safe.directory /fixtures/repos/jenkins-cli.git

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" -o /tmp/go.tar.gz \
  && tar -C /usr/local -xzf /tmp/go.tar.gz \
  && rm /tmp/go.tar.gz \
  && ln -s /usr/local/go/bin/go /usr/local/bin/go \
  && ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt

RUN jenkins-plugin-cli --plugins "workflow-aggregator git configuration-as-code junit job-dsl"

ENV PATH="/usr/local/go/bin:${PATH}"

USER jenkins

ENV JAVA_OPTS="-Djenkins.install.runSetupWizard=false -Dhudson.plugins.git.GitSCM.ALLOW_LOCAL_CHECKOUT=true"
