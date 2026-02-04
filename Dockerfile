FROM golang:1.26rc3-alpine AS builder
LABEL builder=true

RUN adduser --no-create-home --gecos "" --disabled-password user
RUN apk add --update -t build-deps go git mercurial libc-dev gcc libgcc

ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /project
COPY . /project
RUN go build \
    -a \
    -ldflags '-extldflags "-static"' \
    -o /bin/couchdb-cluster-config

FROM scratch
LABEL maintainer="Tobias Gesellchen <tobias@gesellix.de> (@gesellix)"

ENTRYPOINT [ "/couchdb-cluster-config" ]
CMD [ "--help" ]

COPY --from=builder /etc/passwd /etc/passwd
USER user

COPY --from=builder /bin/couchdb-cluster-config /couchdb-cluster-config
