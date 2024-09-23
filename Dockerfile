FROM ubuntu:22.04

RUN apt-get update && \
  apt-get install \
  curl \
  expect \
  jq \
  ssh \
  rsync \
  gettext-base \
  -y


RUN mkdir actions-runner && cd actions-runner && \
  curl -o actions-runner-linux-x64-2.319.1.tar.gz -L https://github.com/actions/runner/releases/download/v2.319.1/actions-runner-linux-x64-2.319.1.tar.gz && \
  tar xzf ./actions-runner-linux-x64-2.319.1.tar.gz && \
  ./bin/installdependencies.sh

COPY ./start.sh /actions-runner/start.sh
RUN chmod +x /actions-runner/start.sh
WORKDIR /actions-runner

CMD ["/bin/bash", "-c", "/actions-runner/start.sh"]
