ARG GO_ALPINE_VARIANT=golang:1.19.0-alpine

# builder
FROM $GO_ALPINE_VARIANT AS builder

WORKDIR /usr/tmp

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
ENV GIN_MODE=release
RUN go build -o scraper src/main.go

# runner
FROM $GO_ALPINE_VARIANT AS runner

RUN apk add --no-cache shadow
ARG USERNAME=user
ARG USER_UID=1001
ARG USER_GID=$USER_UID
RUN addgroup --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME
    # # Add sudo support. Omit if you don't need to install software after connecting.
    # && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    # && chmod 0440 /etc/sudoers.d/$USERNAME
USER $USERNAME

WORKDIR /usr/app
COPY --chown=$USERNAME:$USER_GID --from=builder /usr/tmp/scraper ./
COPY --chown=$USERNAME:$USER_GID --from=builder /usr/tmp/config/config.yml ./config/config.yml

# TODO: port as arg
EXPOSE 8080

CMD ["./scraper"]