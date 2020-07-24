FROM golang:1.13.7-alpine3.11 AS binary
RUN apk -U add openssl git

ADD . /src
WORKDIR /src

RUN CGO_ENABLED=0 go install -ldflags "-X 'main.ver=$(git describe --match='v*' --exact-match)'"

FROM alpine:3.11

COPY --from=binary /go/bin/dockerize /usr/local/bin

ENTRYPOINT ["dockerize"]
CMD ["--help"]

ARG BUILD_DATE
ARG VCS_REF
ARG VERSION
LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.name="Alpinerize" \
      org.label-schema.description="Alpine Dockerize" \
      org.label-schema.url="https://docker.freighttrust.com/" \
      org.label-schema.vcs-ref=$VCS_REF \
      org.label-schema.vcs-url="https://github.com/freight-trust/action-docker.git" \
      org.label-schema.vendor="Freight Trust & Clearing" \
      org.label-schema.version=$VERSION \
      org.label-schema.schema-version="1.0"
