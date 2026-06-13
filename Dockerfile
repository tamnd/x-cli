# Consumed by GoReleaser: it copies the already cross-compiled binary out of the
# build context rather than compiling, so the image build is fast and uses the
# same static binary every other artifact ships.
#
# GoReleaser builds one multi-platform image with buildx and stages each
# platform's binary under a $TARGETPLATFORM directory (e.g. linux/amd64/) in the
# build context, so the COPY line selects the right one through the automatic
# TARGETPLATFORM build arg.
FROM alpine:3.21

ARG TARGETPLATFORM

# ca-certificates for HTTPS to x.com; tzdata for sane timestamps.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 xcli \
 && mkdir -p /data \
 && chown xcli:xcli /data

COPY $TARGETPLATFORM/x /usr/bin/x

USER xcli
WORKDIR /data

# All state lives under /data; mount a volume to keep the cache and session:
#
#   docker run -v ~/data/x:/data ghcr.io/tamnd/x user nasa
ENV X_DATA_DIR=/data
VOLUME ["/data"]

ENTRYPOINT ["/usr/bin/x"]
