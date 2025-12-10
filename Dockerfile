FROM alpine:latest

WORKDIR /opt/manyacg/

# 只保留最基本依赖
RUN apk add --no-cache bash ca-certificates && update-ca-certificates

COPY manyacg .

RUN chmod +x manyacg

ENTRYPOINT ["./manyacg"]
