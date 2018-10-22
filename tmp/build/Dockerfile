FROM alpine:3.6

RUN adduser -D descheduler-operator
USER descheduler-operator

ADD tmp/_output/bin/descheduler-operator /usr/local/bin/descheduler-operator
